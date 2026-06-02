# god

Minimal AI agent framework in Go. Connect WhatsApp, CLI, or any future channel to a configurable AI with persistent memory, personalities (souls), and role-based permissions.

## Quick start

```sh
cp god.example.yaml god.yaml
docker-compose up -d
go install github.com/Djarvur/ddg-search/cmd/ddg-search@latest
export GEMINI_API_KEY=<your-key>
export DATABASE_URL=postgres://god:god@localhost:5432/god

go run . doctor       # verify all deps
go run . cli          # interactive chat
go run . whatsapp     # connect WhatsApp (scan QR on first run)
```

---

## Configuration (`god.yaml`)

### LLM

Global default model, used when a role has no explicit LLM configured.

```yaml
llm:
  model: gemini-2.5-flash-preview-05-20
```

### Connectors

```yaml
connectors:
  whatsapp:
    enabled: true
    store_path: data/whatsapp     # session storage path
    allow:                         # whitelist (empty = allow everyone)
      - "972501234567"
    default_soul: god              # soul for new users before onboarding
    default_role: user             # role assigned to new users
    group_trigger:
      mention_only: false          # only respond when @mentioned in groups
      prefixes: []                 # respond only to messages starting with these

  cli:
    enabled: true
    default_soul: god
    default_role: admin            # CLI defaults to admin
```

### Memory

```yaml
memory:
  top_k: 5             # long-term memories injected per turn
  max_turns: 40        # short-term sliding window (0 = unlimited)
  inactivity_timeout: 30m  # auto-extract memories after this silence (0 = disabled)
```

Short-term memory lives in RAM per user (`connector:userID` key), reset on `/reset` or restart.  
Long-term memory lives in PostgreSQL + pgvector, survives everything except `/factory-reset`.

### Souls

Souls define personality. Each soul has a system prompt and an optional model override.

```yaml
souls:
  default:
    prompt: "You are a helpful assistant."

  god:
    prompt: |
      You are the onboarding agent. Ask new users about themselves,
      then call set_soul to assign the right soul. Be brief.

  human:
    prompt: |
      Write exactly like a human texting. No em dashes. No AI vocabulary.
      Use contractions. Match the user's energy.

  caveman:
    prompt: |
      Speak like caveman. Drop articles, filler, pleasantries.
      Fragments OK. Technical terms exact. Pattern: [thing] [action] [reason].
```

**Built-in souls:**

| Soul | Purpose |
|------|---------|
| `god` | Onboarding — profiles user, assigns soul via `set_soul` tool |
| `human` | Natural human-like conversation, no AI patterns |
| `caveman` | Ultra-compressed, no fluff, developer-friendly |
| `default` | Plain assistant, no personality |

**Soul resolution order:**
1. Per-user assignment in postgres (set via `set_soul` tool or `/soul set`)
2. Connector `default_soul`
3. `"default"`

### Roles

Roles control which LLM a user gets and which tools they can use.

```yaml
roles:
  admin:
    llm:
      provider: gemini
      model: gemini-2.5-flash-preview-05-20
    tools: []           # empty = all tools

  user:
    llm:
      provider: gemini
      model: gemini-2.5-flash-preview-05-20
    tools:
      - calculator
      - web_search
      - remember
      - set_soul
      - search_places

  guest:
    llm:
      provider: gemini
      model: gemini-2.0-flash-lite
    tools:
      - calculator
      - web_search
```

**Role resolution order:**
1. Per-user assignment in postgres
2. `admin` bootstrap list (see below)
3. Connector `default_role`
4. `"user"`

**LLM providers supported:**

| Provider | Value | Key env var |
|----------|-------|-------------|
| Google Gemini | `gemini` | `GEMINI_API_KEY` |
| OpenAI *(coming)* | `openai` | `OPENAI_API_KEY` |
| Anthropic *(coming)* | `anthropic` | `ANTHROPIC_API_KEY` |

### Admin bootstrap

```yaml
admin:
  - "972501234567"   # phone number or userID
```

Users in this list always get admin role, even before a role is assigned in the store. Use this to bootstrap the first admin. After that, use `/factory-reset` and re-onboarding or direct postgres updates.

---

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GEMINI_API_KEY` | Yes | Gemini LLM + embeddings |
| `DATABASE_URL` | Yes (for memory) | Postgres connection string |
| `GOOGLE_PLACES_API_KEY` | No | Enables `search_places` tool |

---

## Commands

Available in any chat by prefixing with `/`:

| Command | Who | Effect |
|---------|-----|--------|
| `/help` | everyone | List available commands |
| `/reset` | everyone | Clear conversation history. Soul, role, and memories kept. |
| `/whoami` | everyone | Show current soul, role, and LLM |
| `/factory-reset` | admin only | Wipe soul, role, all memories, and history |

---

## Tools

Tools are available to the LLM depending on the user's role config.

| Tool | Always on | Requires | Description |
|------|-----------|----------|-------------|
| `calculator` | Yes | — | Evaluate math expressions (`sqrt(144) / 2 + pi`) |
| `web_search` | Yes | `ddg-search` binary | DuckDuckGo web search, no API key |
| `remember` | If DB + embedder | DATABASE_URL | Save a fact to long-term memory |
| `set_soul` | If DB | DATABASE_URL | Assign a soul to the current user |
| `search_places` | Optional | GOOGLE_PLACES_API_KEY | Search nearby places |

---

## CLI flags

```sh
god cli                              # interactive chat as "local"
god cli --msg "hello"                # send one message, print reply, exit
god cli --msg "/whoami"              # run a command non-interactively
god cli --msg "hi" --user alice      # send as user "alice" (created if new)
god cli --user alice                 # interactive chat as "alice"
```

`--user` is useful for testing role/soul assignments per user without needing WhatsApp.

---

## Health check

```sh
god doctor
```

Checks: Gemini API key, `ddg-search` binary, Docker daemon, PostgreSQL connectivity, WhatsApp session.

---

## Architecture

```
cmd/           cobra commands: whatsapp, cli, doctor
agent/         core loop: message → soul/role resolution → LLM → tools → reply
  agent.go     handleMessage, resolveSoul, resolveRole, buildSystemPrompt
command/       slash command registry (/reset, /whoami, /factory-reset, /help)
connector/     WhatsApp (whatsmeow) + CLI
llm/           LLM interface + pool (lazy multi-model cache)
  pool.go      lazy-creates LLM clients by provider:model, pre-warms at startup
embed/gemini/  text-embedding-004 for vector search
store/postgres/ pgvector: soul_assignments, role_assignments, memories
tool/          tool registry + tools
  calculator/  zero-dep recursive descent math parser
  websearch/   shells to ddg-search CLI
  memory/      remember tool
  soul/        set_soul tool
  places/      Google Places
config/        god.yaml with hot-reload (fsnotify)
```

### Message flow

```
user message
  → resolveSoul(store → default_soul → "default")
  → resolveRole(store → admin_list → default_role → "user")
  → llmPool.Get(role.LLM.provider, role.LLM.model)
  → registry.FilteredTools(role.Tools)
  → buildSystemPrompt(soul.Prompt + long-term memories)
  → LLM loop (up to 10 tool rounds)
  → send reply
```
