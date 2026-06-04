# god

Minimal AI agent framework in Go. Run one agent behind many chat front-ends
(WhatsApp, Telegram, CLI) at once, with persistent memory, swappable
personalities (souls), role-based permissions, and any of several LLM providers.

## Quick start

```sh
mkdir -p ~/.god && cp god.example.yaml ~/.god/god.yaml   # starting config
docker-compose up -d                     # postgres + pgvector (for memory)
go install github.com/Djarvur/ddg-search/cmd/ddg-search@latest   # web_search

go run . doctor               # verify deps
go run . model gemini         # set an API key + default model (writes ~/.god/.env)
go run . gateway start        # run the agent behind every enabled connector
go run . cli                  # talk to the running gateway from a second terminal
```

`god` keeps its state in **`~/.god/`** (override with `$GOD_HOME`):
`god.yaml` (config), `.env` (API keys), `whatsapp/` (session), `god.sock`
(control socket), `gateway.lock`, and `god.log`.

Logs go to stderr (human-readable) and are also appended to **`~/.god/god.log`**
as JSON with source locations — review a past run with
`tail -f ~/.god/god.log` or `jq 'select(.level=="ERROR")' ~/.god/god.log`.

---

## Architecture in one breath

`god gateway start` runs **one agent** wrapped by every enabled connector at
once (`multi` connector). An inbound message → resolve **soul** (personality) and
**role** (LLM + allowed tools) for that user → run the LLM tool-loop → reply on
the originating connector. `god cli` is a thin client that talks to the gateway
over a Unix socket, so it shares the same agent, memory, and config.

Connectors run concurrently; the agent serializes per user (`connector:userID`),
so two users — even on different connectors — are handled in parallel while one
user's history stays consistent.

---

## Setup wizards

Two interactive commands write config for you (no hand-editing required):

```sh
god connector            # menu of connectors + status
god connector telegram   # set up a Telegram bot (guided @BotFather + token)
god connector whatsapp   # pair WhatsApp (scan QR; gateway must be stopped)

god model                # menu of LLM providers + status
god model openai         # set OPENAI_API_KEY (validated live) + optional default
```

Connector names and provider names autocomplete. Keys are saved to `~/.god/.env`;
everything else to `~/.god/god.yaml` (comments preserved, backed up to `.bak`).
Enabling a connector or changing the default model takes effect on the next
`god gateway start`.

---

## Configuration (`god.yaml`)

### LLM

Global default, used when a role/soul has no LLM of its own.

```yaml
llm:
  provider: gemini              # gemini | openai | anthropic (empty = gemini)
  model: gemini-3.1-flash-lite
```

| Provider | `provider` | Key (in `~/.god/.env`) |
|----------|------------|------------------------|
| Google Gemini | `gemini` | `GEMINI_API_KEY` |
| OpenAI | `openai` | `OPENAI_API_KEY` |
| Anthropic | `anthropic` / `claude` | `ANTHROPIC_API_KEY` |

Long-term memory (embeddings) is **Gemini-only**, so `GEMINI_API_KEY` is needed
for memory regardless of the chat provider.

### Connectors

```yaml
connectors:
  whatsapp:
    enabled: true
    store_path: ""                 # empty = ~/.god/whatsapp
    allow: ["972501234567"]        # whitelist (empty = everyone); format-tolerant
    default_soul: god
    default_role: user
    group_trigger:
      mention_only: false          # in groups, only reply when @mentioned / prefixed
      prefixes: []

  telegram:
    enabled: false
    token: ""                      # empty = TELEGRAM_BOT_TOKEN env
    allow: ["123456789"]           # numeric Telegram user IDs (empty = everyone)
    default_soul: human
    default_role: user
    group_trigger:
      mention_only: true
      prefixes: []

  cli:
    enabled: true                  # exposes the control socket for `god cli`
    default_soul: god
    default_role: admin
```

### Memory

```yaml
memory:
  top_k: 5                 # long-term memories injected per turn
  max_turns: 40            # short-term sliding window (0 = unlimited)
  inactivity_timeout: 30m  # auto-extract memories after this silence (0 = disabled)
```

Short-term memory is in RAM per user, cleared by `/reset` or restart. Long-term
memory lives in Postgres + pgvector, cleared only by `/factory-reset`.

### Souls

Personalities — each is a system prompt (optional model override). Built-ins:
`god` (onboarding, assigns a soul via `set_soul`), `human`, `caveman`.

```yaml
souls:
  human:
    prompt: |
      Write exactly like a human texting. No em dashes. Use contractions.
```

**Resolution:** per-user (store) → connector `default_soul` → `"human"`.

### Roles

Roles control the LLM and the tool allow-list per user.

```yaml
roles:
  admin:
    llm: { provider: gemini, model: gemini-3.1-flash-lite }
    tools: []                                  # empty = ALL registered tools
  user:
    llm: { provider: openai, model: gpt-4o-mini }
    tools: [web_search, web_extract, remember, set_soul]
  guest:
    llm: { provider: anthropic, model: claude-sonnet-4-6 }
    tools: [web_search, web_extract]
```

**Resolution:** per-user (store) → `admin` bootstrap list → connector
`default_role` → `"user"`. `tools: []` (empty) means **all** tools — list names
to restrict.

### Admin bootstrap

```yaml
admin:
  - "972501234567"   # phone / userID — always gets the admin role
```

---

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GEMINI_API_KEY` | for memory + default Gemini | Gemini LLM + embeddings |
| `OPENAI_API_KEY` | if using OpenAI | OpenAI provider |
| `ANTHROPIC_API_KEY` | if using Anthropic | Anthropic provider |
| `TELEGRAM_BOT_TOKEN` | if telegram token not in yaml | Telegram bot token |
| `DATABASE_URL` | for memory | Postgres connection string |

Keys live in `~/.god/.env` (written by `god model`); shell env overrides it.

---

## Commands (chat, prefix `/`)

| Command | Who | Effect |
|---------|-----|--------|
| `/help` | everyone | List commands |
| `/reset` | everyone | Clear conversation history (soul/role/memories kept) |
| `/whoami` | everyone | Show current soul, role, LLM |
| `/allow add\|remove\|list <number>` | admin | Manage the WhatsApp allow-list (store-backed) |
| `/approve <id>` `/deny <id>` | admin | Resolve a parked tool call (approval gate) |
| `/factory-reset` | admin | Wipe soul, role, all memories, and history |

---

## Tools

Registered tools (gated per role). Some need a binary or config to activate.

| Tool | Requires | Description |
|------|----------|-------------|
| `web_search` | `ddg-search` on PATH | DuckDuckGo search, no API key |
| `web_extract` | — (on by default) | Fetch a URL → markdown, large pages LLM-summarized; SSRF-guarded |
| `remember` | `DATABASE_URL` + embedder | Save a fact to long-term memory |
| `set_soul` | `DATABASE_URL` | Assign a soul to the current user |
| `config` | `tools.config.enabled` | Let god edit `god.yaml` (admin only; pair with `approval`) |
| `read_file` `list_dir` `glob` `grep` `write_file` `edit_file` | `tools.fs.enabled` | Filesystem tools, contained to `tools.fs.root` |

Tools named in `tools.approval` are parked for an admin `/approve` before running.

---

## CLI

```sh
god gateway start                       # run the agent behind all enabled connectors
god cli                                 # interactive chat with the running gateway
god cli --msg "hello"                   # one-shot: send, print reply, exit
god cli --msg "/whoami" --user alice    # run a command as a specific user
god connector [name]                    # set up connectors
god model [provider]                    # set up LLM providers / default model
god doctor                              # health check
```

---

## Health check

```sh
god doctor
```

Checks: Gemini key, `ddg-search`, Docker daemon, Postgres connectivity, WhatsApp
session (`~/.god/whatsapp`), Telegram token (if enabled).

---

## Layout

```
internal/
  cmd/            cobra commands: gateway, cli, connector, model, doctor
  agent/          core loop: message → soul/role → LLM → tools → reply; approval gate
  command/        slash commands (/reset /whoami /allow /approve /factory-reset /help)
  connector/      whatsapp, telegram, socket (cli transport), multi (fan-out)
    setup/        connector setup wizards (registry + huh)
  llm/            LLM interface + pool; gemini / openai / anthropic adapters
    setup/        model setup wizards (registry + huh)
  embed/gemini/   text-embedding-004 for vector search
  store/postgres/ pgvector: soul/role assignments, memories, allow-list
  tools/          provider-neutral tool ecosystem (web_search, web_extract, fs, …)
  config/         god.yaml load + hot-reload (fsnotify) + comment-preserving edit
  godhome/        ~/.god paths, .env writer, gateway lock
```
