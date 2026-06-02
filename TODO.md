# god — project status & TODO

## Done ✓

- [x] WhatsApp + CLI connectors (interactive + one-shot `--msg`/`--user`)
- [x] Gemini LLM adapter (tool calls, thought signatures, pool by provider:model)
- [x] Short-term memory: in-memory sliding window (`max_turns`)
- [x] Long-term memory: pgvector, `remember` tool + inactivity timer auto-extraction
- [x] **Souls**: `god` (onboarding), `human` (humanizer rules), `caveman` (ultra-compressed), `default`
- [x] **Roles**: admin/user/guest — controls LLM + tool access per user
- [x] **LLM pool**: lazy multi-model cache, pre-warmed at startup, provider:model keyed
- [x] **Config hot-reload**: `Supplier()` pattern — all components read latest yaml per-operation
- [x] **God soul onboarding**: new users profiled → `set_soul` tool → permanent soul assigned
- [x] Command registry: `/reset`, `/whoami`, `/factory-reset`, `/help`
- [x] Tools: calculator, web_search (ddg-search), remember, set_soul, search_places
- [x] `god doctor` health check
- [x] `god cli --msg "..." --user "..."` one-shot mode
- [x] Role-based admin: `/factory-reset` wipes soul+role+memories+history
- [x] Integration tests (8): onboarding, whoami, reset, factory-reset, memory injection, tool filtering, LLM routing, multi-user isolation
- [x] README.md + INSTALL.md

## TODO (priority order)

### 1. Multi-LLM providers
Add OpenAI + Anthropic to `buildLLMPool` factory in `cmd/common.go`:
```go
case "openai":
    return openai.New(ctx, os.Getenv("OPENAI_API_KEY"), pcfg.Model)
case "anthropic":
    return anthropic.New(ctx, os.Getenv("ANTHROPIC_API_KEY"), pcfg.Model)
```
Create `llm/openai/` and `llm/anthropic/` adapters implementing `llm.LLM`.

### 2. Role assignment command
`/role set <name>` — admin only, persists to store.
Add to `command/builtin.go` using `Runtime.AssignRole` callback.

### 3. Telegram connector
`connector/telegram/` implementing `connector.Connector` interface.
Token via `TELEGRAM_BOT_TOKEN`. Wire in `cmd/telegram.go`.

### 4. MCP support
`tool/mcp/` — spin up MCP servers, proxy tool calls.
Gives instant access to filesystem, GitHub, databases ecosystem.

### 5. Media/images
`connector.Message` needs `Media []byte` + `MimeType string`.
WhatsApp sends images → Gemini vision handles them.

### 6. Rate limiting / quotas per role
Guest role: max N messages/hour. Configurable in role config.

---

## Config quick reference

```yaml
llm:
  model: gemini-3.1-flash-lite   # global default

memory:
  top_k: 5
  max_turns: 40
  inactivity_timeout: 30m

admin:
  - "972526777236"   # bootstrap admins (before role store assignment)

connectors:
  whatsapp:
    default_soul: god
    default_role: user
  cli:
    default_soul: god
    default_role: admin

roles:
  admin:
    llm: { provider: gemini, model: gemini-3.1-flash-lite }
    tools: []         # empty = all tools
  user:
    llm: { provider: gemini, model: gemini-3.1-flash-lite }
    tools: [calculator, web_search, remember, set_soul, search_places]
  guest:
    llm: { provider: gemini, model: gemini-3.1-flash-lite }
    tools: [calculator, web_search]

souls:
  god:     { prompt: "onboarding agent..." }
  human:   { prompt: "human-like, no AI patterns..." }
  caveman: { prompt: "drop articles/filler, fragments OK..." }
  default: { prompt: "You are a helpful assistant." }
```
