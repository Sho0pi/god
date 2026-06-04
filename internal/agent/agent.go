package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/sho0pi/god/internal/command"
	"github.com/sho0pi/god/internal/config"
	"github.com/sho0pi/god/internal/connector"
	"github.com/sho0pi/god/internal/embed"
	"github.com/sho0pi/god/internal/llm"
	"github.com/sho0pi/god/internal/store"
	toolpkg "github.com/sho0pi/god/internal/tools"
)

const maxToolRounds = 10

// defaultSoul is the fallback personality when none is assigned and no connector
// default applies.
const defaultSoul = "human"

const extractionSystemPrompt = "Extract important facts about this user from the conversation history. " +
	"Write one fact per line. Facts must be concrete and worth remembering across sessions: " +
	"preferences, background, ongoing projects, names, goals. " +
	"If nothing is worth extracting, reply with an empty response."

// Options configures an Agent.
type Options struct {
	TopK              int
	MaxTurns          int
	InactivityTimeout time.Duration
	Commands          *command.Registry
	// ConfigFn, when set, is called per-message for souls/roles/admins/topK.
	// Changes to god.yaml take effect immediately. Overrides the static fields below.
	ConfigFn      func() *config.Config
	ConnectorName string // required when ConfigFn is set to read connector defaults
	// Static fallbacks used when ConfigFn is nil (tests, programmatic use).
	Souls        map[string]config.SoulConfig
	DefaultSouls map[string]string
	Roles        map[string]config.RoleConfig
	DefaultRoles map[string]string
	Admins       []string
	LLMPool      *llm.Pool
}

// Agent routes messages to the LLM, manages per-user history and long-term memory.
type Agent struct {
	connector         connector.Connector
	llm               llm.LLM // default fallback
	llmPool           *llm.Pool
	registry          *toolpkg.Registry
	cmdRegistry       *command.Registry
	embedder          embed.Embedder
	store             store.Store
	maxTurns          int
	inactivityTimeout time.Duration
	connectorName     string
	// configFn is the single source of souls/roles/admins/topK, read live per
	// message. New synthesises one from static Options when none is supplied.
	configFn func() *config.Config
	// Per-connector defaults for arbitrary connector names (config.Config only
	// types whatsapp/cli). Used as a fallback in soul/role resolution.
	defaultSouls map[string]string
	defaultRoles map[string]string

	historyMu sync.Mutex
	history   map[string][]llm.Message

	// userMu serializes message handling per user so concurrent messages from
	// the same user (whatsapp dispatches each in its own goroutine) can't
	// clobber each other's history read-modify-write.
	userMuMu sync.Mutex
	userMu   map[string]*sync.Mutex

	timerMu sync.Mutex
	timers  map[string]*time.Timer

	pendingMu sync.Mutex
	pending   map[string]*pendingApproval
}

// lockUser acquires the per-user lock, creating it on first use, and returns
// the unlock func. Different users never block each other.
func (a *Agent) lockUser(key string) func() {
	a.userMuMu.Lock()
	mu, ok := a.userMu[key]
	if !ok {
		mu = &sync.Mutex{}
		a.userMu[key] = mu
	}
	a.userMuMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

func New(c connector.Connector, l llm.LLM, r *toolpkg.Registry, e embed.Embedder, s store.Store, opts Options) *Agent {
	cmdReg := opts.Commands
	if cmdReg == nil {
		cmdReg = command.NewRegistry(command.Builtin())
	}
	pool := opts.LLMPool
	if pool == nil {
		pool = llm.NewPool(nil, l)
	}
	// Single config path: use the supplier if given, else synthesise one from
	// the static Options so callers (mainly tests) need not build a Loader.
	configFn := opts.ConfigFn
	if configFn == nil {
		static := &config.Config{
			Memory: config.MemoryConfig{TopK: opts.TopK},
			Souls:  opts.Souls,
			Roles:  opts.Roles,
			Admin:  opts.Admins,
		}
		configFn = func() *config.Config { return static }
	}
	return &Agent{
		connector:         c,
		llm:               l,
		llmPool:           pool,
		registry:          r,
		cmdRegistry:       cmdReg,
		embedder:          e,
		store:             s,
		maxTurns:          opts.MaxTurns,
		inactivityTimeout: opts.InactivityTimeout,
		configFn:          configFn,
		connectorName:     opts.ConnectorName,
		defaultSouls:      opts.DefaultSouls,
		defaultRoles:      opts.DefaultRoles,
		history:           make(map[string][]llm.Message),
		userMu:            make(map[string]*sync.Mutex),
		timers:            make(map[string]*time.Timer),
		pending:           make(map[string]*pendingApproval),
	}
}

// liveConfig returns the current config snapshot from the supplier.
func (a *Agent) liveConfig() *config.Config {
	return a.configFn()
}

func (a *Agent) Run(ctx context.Context) error {
	a.connector.SetMessageHandler(a.handleMessage)
	if err := a.connector.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()

	a.timerMu.Lock()
	for _, t := range a.timers {
		t.Stop()
	}
	a.timerMu.Unlock()

	return a.connector.Stop(context.Background())
}

func (a *Agent) handleMessage(ctx context.Context, msg connector.Message) {
	userKey := msg.Connector + ":" + msg.UserID

	if strings.HasPrefix(msg.Text, "/") {
		a.handleCommand(ctx, userKey, msg)
		return
	}

	// Serialize concurrent messages from the same user. The history
	// read-modify-write below spans slow LLM calls; without this two
	// messages would race the backing slice and clobber each other's turns.
	unlock := a.lockUser(userKey)
	defer unlock()

	// Block normal messages while an approval is pending — the admin must
	// resolve it first so god doesn't lose the parked action.
	if p := a.getPending(userKey); p != nil {
		a.sendOrLog(ctx, msg.ChatID, fmt.Sprintf(
			"You have a pending approval (%s). Reply /approve %s or /deny %s first.", p.id, p.id, p.id))
		return
	}

	a.resetInactivityTimer(userKey, msg.Connector, msg.UserID)

	ctx = context.WithValue(ctx, toolpkg.UserKey{}, toolpkg.UserInfo{
		Connector: msg.Connector,
		UserID:    msg.UserID,
	})

	soulName := a.resolveSoul(ctx, msg)
	roleName := a.resolveRole(ctx, msg)
	roleCfg := a.getRoleConfig(roleName)
	msgLLM := a.llmPool.Get(ctx, llm.ProviderConfig{
		Provider: roleCfg.LLM.Provider,
		Model:    roleCfg.LLM.Model,
	})
	tools := a.registry.FilteredTools(roleCfg.Tools)
	systemPrompt := a.buildSystemPrompt(ctx, msg, soulName)

	slog.Info("agent", "soul", soulName, "role", roleName, "provider", roleCfg.LLM.Provider, "model", roleCfg.LLM.Model, "connector", msg.Connector, "user", msg.UserID)

	a.historyMu.Lock()
	hist := append(a.history[userKey], llm.Message{Role: "user", Text: msg.Text})
	a.historyMu.Unlock()

	a.runToolLoop(ctx, userKey, msg.Connector, msg.UserID, msg.ChatID, hist, systemPrompt, tools, msgLLM)
}

// runToolLoop drives the LLM ↔ tool conversation until a final text answer.
// Caller must hold the per-user lock. When a tool requires approval, the loop
// parks its state (see pendingApproval) and returns; resumeApproval re-enters.
func (a *Agent) runToolLoop(ctx context.Context, userKey, connectorName, userID, chatID string, hist []llm.Message, systemPrompt string, tools []toolpkg.Tool, msgLLM llm.LLM) {
	for round := 0; round < maxToolRounds; round++ {
		resp, err := msgLLM.ChatWithSystem(ctx, systemPrompt, hist, tools)
		if err != nil {
			slog.Error("llm error", "err", err)
			return
		}

		if resp.ToolCall == nil {
			a.historyMu.Lock()
			a.history[userKey] = trimHistory(
				append(hist, llm.Message{Role: "model", Text: resp.Text}),
				a.maxTurns,
			)
			a.historyMu.Unlock()

			a.sendOrLog(ctx, chatID, resp.Text)
			return
		}

		tc := resp.ToolCall
		slog.Debug("tool use", "tool", tc.Name, "user", userKey)

		// Approval gate: park sensitive tool calls and wait for /approve.
		if a.needsApproval(tc.Name) {
			id := newApprovalID()
			a.setPending(userKey, &pendingApproval{
				id:           id,
				connector:    connectorName,
				userID:       userID,
				chatID:       chatID,
				toolCall:     tc,
				hist:         append(hist, llm.Message{Role: "model", ToolCall: tc}),
				systemPrompt: systemPrompt,
				tools:        tools,
				llm:          msgLLM,
				expires:      time.Now().Add(approvalTTL),
			})
			slog.Info("approval required", "tool", tc.Name, "id", id, "user", userKey)
			a.sendOrLog(ctx, chatID, fmt.Sprintf(
				"⚠️ Approval required — god wants to run %q:\n%s\n\nReply /approve %s  or  /deny %s",
				tc.Name, previewToolCall(tc), id, id))
			return
		}

		res, err := a.registry.Dispatch(ctx, tc.Name, tc.Args)
		result := res.Content
		if err != nil {
			slog.Error("tool error", "tool", tc.Name, "err", err)
			result = "error: " + err.Error()
		}

		slog.Debug("tool result", "tool", tc.Name, "result", truncate(result, 80))

		hist = append(hist,
			llm.Message{Role: "model", ToolCall: tc},
			llm.Message{ToolResult: &llm.ToolResult{
				Name:             tc.Name,
				Result:           result,
				ThoughtSignature: tc.ThoughtSignature,
			}},
		)
	}

	slog.Warn("agent: reached max tool rounds", "max", maxToolRounds)
}

func (a *Agent) sendOrLog(ctx context.Context, chatID, text string) {
	if err := a.connector.Send(ctx, chatID, text); err != nil {
		slog.Error("send error", "err", err)
	}
}

func (a *Agent) handleCommand(ctx context.Context, userKey string, msg connector.Message) {
	text := strings.TrimSpace(msg.Text)
	name := strings.TrimPrefix(text, "/")
	if idx := strings.IndexByte(name, ' '); idx >= 0 {
		name = name[:idx]
	}

	req := command.Request{
		Text:      text,
		ChatID:    msg.ChatID,
		UserID:    msg.UserID,
		Connector: msg.Connector,
		Reply: func(reply string) error {
			return a.connector.Send(ctx, msg.ChatID, reply)
		},
	}

	def, ok := a.cmdRegistry.Lookup(name)
	if !ok {
		if err := req.Reply("Unknown command. Type /help for available commands."); err != nil {
			slog.Error("send error", "err", err)
		}
		return
	}

	roleName := a.resolveRole(ctx, msg)
	rt := &cmdSession{
		a:        a,
		ctx:      ctx,
		msg:      msg,
		userKey:  userKey,
		roleName: roleName,
		soulName: a.resolveSoul(ctx, msg),
		roleCfg:  a.getRoleConfig(roleName),
	}

	if err := def.Handler(ctx, req, rt); err != nil {
		slog.Error("command error", "cmd", def.Name, "err", err)
	}
}

// clearUserHistory stops the inactivity timer and drops short-term history for
// a user. Used by /reset and /factory-reset.
func (a *Agent) clearUserHistory(userKey string) {
	a.timerMu.Lock()
	if t, ok := a.timers[userKey]; ok {
		t.Stop()
		delete(a.timers, userKey)
	}
	a.timerMu.Unlock()
	a.historyMu.Lock()
	delete(a.history, userKey)
	a.historyMu.Unlock()
}

// resolveSoul returns: store → connector default (from live config) → defaultSoul.
func (a *Agent) resolveSoul(ctx context.Context, msg connector.Message) string {
	if a.store != nil {
		name, err := a.store.GetSoul(ctx, msg.Connector, msg.UserID)
		if err != nil {
			slog.Warn("resolve soul", "err", err)
		} else if name != "" {
			return name
		}
	}
	cfg := a.liveConfig()
	switch msg.Connector {
	case "whatsapp":
		if cfg.Connectors.WhatsApp.DefaultSoul != "" {
			return cfg.Connectors.WhatsApp.DefaultSoul
		}
	case "telegram":
		if cfg.Connectors.Telegram.DefaultSoul != "" {
			return cfg.Connectors.Telegram.DefaultSoul
		}
	case "cli":
		if cfg.Connectors.CLI.DefaultSoul != "" {
			return cfg.Connectors.CLI.DefaultSoul
		}
	}
	if a.defaultSouls != nil {
		if name, ok := a.defaultSouls[msg.Connector]; ok && name != "" {
			return name
		}
	}
	return defaultSoul
}

// resolveRole returns: store → admin bootstrap list → connector default (from live config) → "user".
func (a *Agent) resolveRole(ctx context.Context, msg connector.Message) string {
	if a.store != nil {
		name, err := a.store.GetRole(ctx, msg.Connector, msg.UserID)
		if err != nil {
			slog.Warn("resolve role", "err", err)
		} else if name != "" {
			return name
		}
	}
	cfg := a.liveConfig()
	// Admin bootstrap: config admin list.
	for _, id := range cfg.Admin {
		if id == msg.UserID {
			return "admin"
		}
	}
	switch msg.Connector {
	case "whatsapp":
		if cfg.Connectors.WhatsApp.DefaultRole != "" {
			return cfg.Connectors.WhatsApp.DefaultRole
		}
	case "telegram":
		if cfg.Connectors.Telegram.DefaultRole != "" {
			return cfg.Connectors.Telegram.DefaultRole
		}
	case "cli":
		if cfg.Connectors.CLI.DefaultRole != "" {
			return cfg.Connectors.CLI.DefaultRole
		}
	}
	if a.defaultRoles != nil {
		if name, ok := a.defaultRoles[msg.Connector]; ok && name != "" {
			return name
		}
	}
	return "user"
}

func (a *Agent) getRoleConfig(roleName string) config.RoleConfig {
	if cfg := a.liveConfig(); cfg.Roles != nil {
		if rc, ok := cfg.Roles[roleName]; ok {
			return rc
		}
	}
	return config.RoleConfig{} // empty = all tools, default LLM
}

func (a *Agent) resetInactivityTimer(userKey, connectorName, userID string) {
	if a.inactivityTimeout == 0 {
		return
	}
	a.timerMu.Lock()
	defer a.timerMu.Unlock()

	if t, ok := a.timers[userKey]; ok {
		t.Stop()
	}
	a.timers[userKey] = time.AfterFunc(a.inactivityTimeout, func() {
		a.historyMu.Lock()
		hist := make([]llm.Message, len(a.history[userKey]))
		copy(hist, a.history[userKey])
		a.historyMu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		a.extractAndSave(ctx, connectorName, userID, hist)
	})
}

func (a *Agent) extractAndSave(ctx context.Context, connectorName, userID string, hist []llm.Message) {
	if a.embedder == nil || a.store == nil || len(hist) == 0 {
		return
	}
	resp, err := a.llm.ChatWithSystem(ctx, extractionSystemPrompt, hist, nil)
	if err != nil {
		slog.Error("memory extraction: llm error", "err", err)
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(resp.Text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		embedding, err := a.embedder.Embed(ctx, line)
		if err != nil {
			slog.Error("memory extraction: embed", "fact", line, "err", err)
			continue
		}
		if err := a.store.SaveMemory(ctx, connectorName, userID, line, embedding); err != nil {
			slog.Error("memory extraction: save", "fact", line, "err", err)
			continue
		}
		slog.Debug("memory extraction: saved", "fact", line, "connector", connectorName, "user", userID)
	}
}

func (a *Agent) buildSystemPrompt(ctx context.Context, msg connector.Message, soulName string) string {
	cfg := a.liveConfig()
	base := "You are a helpful assistant."
	if soul, ok := cfg.Souls[soulName]; ok && soul.Prompt != "" {
		base = soul.Prompt
	}

	if a.embedder == nil || a.store == nil {
		return base
	}

	embedding, err := a.embedder.Embed(ctx, msg.Text)
	if err != nil {
		slog.Error("embed error", "err", err)
		return base
	}

	topK := cfg.Memory.TopK
	if topK == 0 {
		topK = 5
	}
	facts, err := a.store.SearchMemories(ctx, msg.Connector, msg.UserID, embedding, topK)
	if err != nil {
		slog.Error("memory search error", "err", err)
		return base
	}

	if len(facts) == 0 {
		return base
	}

	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteString("\n\nThings you know about this person:\n")
	for _, f := range facts {
		fmt.Fprintf(&sb, "- %s\n", f)
	}
	return sb.String()
}

func trimHistory(hist []llm.Message, maxTurns int) []llm.Message {
	if maxTurns <= 0 {
		return hist
	}
	cap := maxTurns * 2
	if len(hist) > cap {
		return hist[len(hist)-cap:]
	}
	return hist
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
