package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/sho0pi/god/command"
	"github.com/sho0pi/god/config"
	"github.com/sho0pi/god/connector"
	"github.com/sho0pi/god/embed"
	"github.com/sho0pi/god/llm"
	"github.com/sho0pi/god/store"
	"github.com/sho0pi/god/tool"
	"github.com/sho0pi/god/tool/memory"
)

const maxToolRounds = 10

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
	registry          *tool.Registry
	cmdRegistry       *command.Registry
	embedder          embed.Embedder
	store             store.Store
	maxTurns          int
	inactivityTimeout time.Duration
	connectorName     string
	// configFn, when non-nil, is the live-config supplier. Takes priority over static fields.
	configFn func() *config.Config
	// Static fields used when configFn is nil.
	topK         int
	souls        map[string]config.SoulConfig
	defaultSouls map[string]string
	roles        map[string]config.RoleConfig
	defaultRoles map[string]string
	admins       map[string]struct{}

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

func New(c connector.Connector, l llm.LLM, r *tool.Registry, e embed.Embedder, s store.Store, opts Options) *Agent {
	cmdReg := opts.Commands
	if cmdReg == nil {
		cmdReg = command.NewRegistry(command.Builtin())
	}
	admins := make(map[string]struct{}, len(opts.Admins))
	for _, id := range opts.Admins {
		admins[id] = struct{}{}
	}
	pool := opts.LLMPool
	if pool == nil {
		pool = llm.NewPool(nil, l)
	}
	return &Agent{
		connector:         c,
		llm:               l,
		llmPool:           pool,
		registry:          r,
		cmdRegistry:       cmdReg,
		embedder:          e,
		store:             s,
		topK:              opts.TopK,
		maxTurns:          opts.MaxTurns,
		inactivityTimeout: opts.InactivityTimeout,
		configFn:          opts.ConfigFn,
		connectorName:     opts.ConnectorName,
		souls:             opts.Souls,
		defaultSouls:      opts.DefaultSouls,
		roles:             opts.Roles,
		defaultRoles:      opts.DefaultRoles,
		admins:            admins,
		history:           make(map[string][]llm.Message),
		userMu:            make(map[string]*sync.Mutex),
		timers:            make(map[string]*time.Timer),
		pending:           make(map[string]*pendingApproval),
	}
}

// liveConfig returns the current config snapshot. If a supplier is set, reads from it;
// otherwise synthesises a minimal Config from static Options fields.
func (a *Agent) liveConfig() *config.Config {
	if a.configFn != nil {
		return a.configFn()
	}
	return &config.Config{
		Memory: config.MemoryConfig{TopK: a.topK},
		Souls:  a.souls,
		Roles:  a.roles,
		Admin: func() []string {
			out := make([]string, 0, len(a.admins))
			for id := range a.admins {
				out = append(out, id)
			}
			return out
		}(),
	}
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

	ctx = context.WithValue(ctx, memory.UserKey{}, memory.UserInfo{
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

	log.Printf("agent: soul=%q role=%q llm=%s/%s user=%s:%s",
		soulName, roleName, roleCfg.LLM.Provider, roleCfg.LLM.Model,
		msg.Connector, msg.UserID)

	a.historyMu.Lock()
	hist := append(a.history[userKey], llm.Message{Role: "user", Text: msg.Text})
	a.historyMu.Unlock()

	a.runToolLoop(ctx, userKey, msg.Connector, msg.UserID, msg.ChatID, hist, systemPrompt, tools, msgLLM)
}

// runToolLoop drives the LLM ↔ tool conversation until a final text answer.
// Caller must hold the per-user lock. When a tool requires approval, the loop
// parks its state (see pendingApproval) and returns; resumeApproval re-enters.
func (a *Agent) runToolLoop(ctx context.Context, userKey, connectorName, userID, chatID string, hist []llm.Message, systemPrompt string, tools []tool.Tool, msgLLM llm.LLM) {
	for round := 0; round < maxToolRounds; round++ {
		resp, err := msgLLM.ChatWithSystem(ctx, systemPrompt, hist, tools)
		if err != nil {
			log.Printf("llm error: %v", err)
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
		log.Printf("tool call: %s %v", tc.Name, tc.Args)

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
			log.Printf("approval required: tool=%s id=%s user=%s", tc.Name, id, userKey)
			a.sendOrLog(ctx, chatID, fmt.Sprintf(
				"⚠️ Approval required — god wants to run %q:\n%s\n\nReply /approve %s  or  /deny %s",
				tc.Name, previewToolCall(tc), id, id))
			return
		}

		result, err := a.registry.Dispatch(ctx, tc.Name, tc.Args)
		if err != nil {
			log.Printf("tool error: %v", err)
			result = "error: " + err.Error()
		}

		log.Printf("tool result: %s → %s", tc.Name, truncate(result, 80))

		hist = append(hist,
			llm.Message{Role: "model", ToolCall: tc},
			llm.Message{ToolResult: &llm.ToolResult{
				Name:             tc.Name,
				Result:           result,
				ThoughtSignature: tc.ThoughtSignature,
			}},
		)
	}

	log.Printf("agent: reached max tool rounds (%d)", maxToolRounds)
}

func (a *Agent) sendOrLog(ctx context.Context, chatID, text string) {
	if err := a.connector.Send(ctx, chatID, text); err != nil {
		log.Printf("send error: %v", err)
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
			log.Printf("send error: %v", err)
		}
		return
	}

	soulName := a.resolveSoul(ctx, msg)
	roleName := a.resolveRole(ctx, msg)
	roleCfg := a.getRoleConfig(roleName)

	clearHistory := func() error {
		a.timerMu.Lock()
		if t, ok := a.timers[userKey]; ok {
			t.Stop()
			delete(a.timers, userKey)
		}
		a.timerMu.Unlock()
		a.historyMu.Lock()
		delete(a.history, userKey)
		a.historyMu.Unlock()
		return nil
	}

	rt := &command.Runtime{
		ClearHistory: clearHistory,
		IsAdmin: func() bool {
			if roleName == "admin" {
				return true
			}
			_, ok := a.admins[msg.UserID]
			return ok
		},
		FactoryReset: func() error {
			if err := clearHistory(); err != nil {
				return err
			}
			if a.store == nil {
				return nil
			}
			if err := a.store.DeleteSoul(ctx, msg.Connector, msg.UserID); err != nil {
				return fmt.Errorf("delete soul: %w", err)
			}
			if err := a.store.DeleteRole(ctx, msg.Connector, msg.UserID); err != nil {
				return fmt.Errorf("delete role: %w", err)
			}
			if err := a.store.DeleteMemories(ctx, msg.Connector, msg.UserID); err != nil {
				return fmt.Errorf("delete memories: %w", err)
			}
			return nil
		},
		GetInfo: func() command.UserInfo {
			return command.UserInfo{
				Soul:     soulName,
				Role:     roleName,
				LLMModel: roleCfg.LLM.Model,
				Provider: roleCfg.LLM.Provider,
			}
		},
	}

	if a.store != nil {
		rt.AllowAdd = func(number string) error {
			return a.store.AddAllow(ctx, msg.Connector, number)
		}
		rt.AllowRemove = func(number string) error {
			return a.store.RemoveAllow(ctx, msg.Connector, number)
		}
		rt.AllowList = func() ([]string, error) {
			return a.store.ListAllow(ctx, msg.Connector)
		}
	}

	rt.ResolveApproval = func(approve bool, id string) {
		a.resumeApproval(ctx, userKey, msg.ChatID, approve, id)
	}

	if err := def.Handler(ctx, req, rt); err != nil {
		log.Printf("command /%s error: %v", def.Name, err)
	}
}

// resolveSoul returns: store → connector default (from live config) → "default".
func (a *Agent) resolveSoul(ctx context.Context, msg connector.Message) string {
	if a.store != nil {
		name, err := a.store.GetSoul(ctx, msg.Connector, msg.UserID)
		if err != nil {
			log.Printf("resolve soul: %v", err)
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
	return "default"
}

// resolveRole returns: store → admin bootstrap list → connector default (from live config) → "user".
func (a *Agent) resolveRole(ctx context.Context, msg connector.Message) string {
	if a.store != nil {
		name, err := a.store.GetRole(ctx, msg.Connector, msg.UserID)
		if err != nil {
			log.Printf("resolve role: %v", err)
		} else if name != "" {
			return name
		}
	}
	cfg := a.liveConfig()
	// Admin bootstrap: check live config admin list + static admins map.
	for _, id := range cfg.Admin {
		if id == msg.UserID {
			return "admin"
		}
	}
	if _, ok := a.admins[msg.UserID]; ok {
		return "admin"
	}
	switch msg.Connector {
	case "whatsapp":
		if cfg.Connectors.WhatsApp.DefaultRole != "" {
			return cfg.Connectors.WhatsApp.DefaultRole
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
	if a.roles != nil {
		if rc, ok := a.roles[roleName]; ok {
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
		log.Printf("memory extraction: llm error: %v", err)
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(resp.Text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		embedding, err := a.embedder.Embed(ctx, line)
		if err != nil {
			log.Printf("memory extraction: embed %q: %v", line, err)
			continue
		}
		if err := a.store.SaveMemory(ctx, connectorName, userID, line, embedding); err != nil {
			log.Printf("memory extraction: save %q: %v", line, err)
			continue
		}
		log.Printf("memory extraction: saved %q for %s:%s", line, connectorName, userID)
	}
}

func (a *Agent) buildSystemPrompt(ctx context.Context, msg connector.Message, soulName string) string {
	cfg := a.liveConfig()
	base := "You are a helpful assistant."
	if cfg.Souls != nil {
		if soul, ok := cfg.Souls[soulName]; ok && soul.Prompt != "" {
			base = soul.Prompt
		}
	} else if a.souls != nil {
		if soul, ok := a.souls[soulName]; ok && soul.Prompt != "" {
			base = soul.Prompt
		}
	}

	if a.embedder == nil || a.store == nil {
		return base
	}

	embedding, err := a.embedder.Embed(ctx, msg.Text)
	if err != nil {
		log.Printf("embed error: %v", err)
		return base
	}

	topK := a.topK
	if topK == 0 {
		topK = cfg.Memory.TopK
	}
	if topK == 0 {
		topK = 5
	}
	facts, err := a.store.SearchMemories(ctx, msg.Connector, msg.UserID, embedding, topK)
	if err != nil {
		log.Printf("memory search error: %v", err)
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
