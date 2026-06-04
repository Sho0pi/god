# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
make build          # go build ./...
make test           # go test ./...
make race           # go test -race ./...   (use this — the agent is concurrent)
make lint           # golangci-lint run (v2 config; CI requires it green)
make fmt            # gofmt -w .  (CI fails on unformatted files)
make doctor         # build + run `god doctor` (checks all external deps)

go test ./internal/agent/ -run TestApprovalGate_Approve -v   # single test
go run . cli                      # interactive chat
go run . cli --msg "hi" --user x  # one-shot, prints reply, exits
go run . whatsapp                 # WhatsApp connector (scan QR first run)
```

Some tests touch real services and **skip** when unavailable: `internal/tool/exec` needs a Docker daemon (it runs `alpine:3.20` — `docker pull alpine:3.20` first); the postgres store tests need `DATABASE_URL` (`docker-compose up -d` brings up postgres+pgvector). `web_search` shells out to the `ddg-search` binary.

## Architecture

god routes chat messages from a **connector** to an **LLM**, runs **tools** the LLM requests, and persists **memory**. All non-`main` code lives under `internal/`.

**Message flow** (`internal/agent/agent.go`): connector receives a message → `handleMessage` → resolve **soul** (personality/system prompt) and **role** (which LLM + which tools) for that user → `runToolLoop`: call LLM, if it returns a tool call dispatch it via the registry and loop, else send the final text. `/`-prefixed messages go to `handleCommand` instead.

**Souls and roles are resolved per message, not per session.** Resolution order — soul: store → connector default → `"default"`; role: store → `config.Admin` bootstrap list → connector default → `"user"`. A role's `tools: []` (empty) means **all tools**; list names to restrict. This is why enabling a powerful tool (`exec`, `config`) without explicit role tool-lists hands it to everyone.

**Live config (the central pattern).** `config.Loader.Supplier()` returns a `func() *config.Config` read **on every message**, so `god.yaml` edits take effect with no restart (viper + fsnotify hot-reload). The agent holds only this `configFn` — there is one config source of truth. `cmd` builds an `app` struct (loader/cfg/cfgFile) in cobra's `PersistentPreRunE` and carries it via command context (`appFrom(cmd)`); there are no package-level config globals.

**Tools** (`internal/tool/`) implement the `tool.Tool` interface and are gathered in a `Registry` (`FilteredTools(allowed)` enforces per-role access). Notable ones: `exec` runs shell commands inside a throwaway, locked-down Docker container (no host FS, no network by default — the prompt-injection boundary); `config` lets god edit `god.yaml` only, validated + backed up to `god.yaml.bak` + hot-reloaded. Both are disabled by default and admin-only.

**Approval gate** (`internal/agent/approval.go`): tools named in `tools.approval` are not run immediately — the loop **parks** the call (saving full state in `pendingApproval`), shows a preview, and waits for an admin `/approve <id>` or `/deny <id>` (10-min TTL). `resumeApproval` dispatches-or-rejects and re-enters `runToolLoop`. Because `/approve` is a *separate* message, the parked state must carry everything needed to resume.

**Interfaces at every seam** — `connector.Connector`, `llm.LLM`, `embed.Embedder`, `tool.Tool`, and the split `store.{Soul,Role,Memory,Allow}Store` (consumers depend on the narrowest one). Adding a provider/connector/tool is implementing the interface; for a new LLM provider also add a case to the factory in `internal/cmd/common.go`'s `buildLLMPool` (currently Gemini only). The `llm.Pool` lazily caches clients by `provider:model`.

**Memory.** Short-term = in-memory per-user sliding window (`max_turns`), cleared by `/reset`. Long-term = pgvector: written by the `remember` tool or an inactivity-timer extraction pass, injected into the system prompt by cosine similarity (`top_k`), cleared only by `/factory-reset`.

## Conventions

- The whatsapp connector dispatches each inbound message in its own goroutine, so the agent serializes per user with `lockUser(userKey)` — hold it for any history read-modify-write. Run tests with `-race`.
- WhatsApp allow-list matching is format-tolerant (`phoneMatch`): strips leading zeros + suffix-matches, so local `0501234567` equals international `972501234567`.
- `command.Runtime` is an interface implemented by `agent.cmdSession`; capabilities missing in the current config (e.g. allow-list ops with no store) return `command.ErrUnsupported` rather than being nil.
- `god.yaml` is gitignored (holds real admin/allow data); edit `god.example.yaml` for documented defaults.
