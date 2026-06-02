package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/sho0pi/god/connector"
	"github.com/sho0pi/god/embed"
	"github.com/sho0pi/god/llm"
	"github.com/sho0pi/god/store"
	"github.com/sho0pi/god/tool"
	"github.com/sho0pi/god/tool/memory"
)

const maxToolRounds = 10

// Agent routes messages to the LLM, manages per-user history and long-term memory.
type Agent struct {
	connector connector.Connector
	llm       llm.LLM
	registry  *tool.Registry
	embedder  embed.Embedder
	store     store.Store
	topK      int

	historyMu sync.Mutex
	history   map[string][]llm.Message // key: "connector:userID"
}

func New(c connector.Connector, l llm.LLM, r *tool.Registry, e embed.Embedder, s store.Store, topK int) *Agent {
	return &Agent{
		connector: c,
		llm:       l,
		registry:  r,
		embedder:  e,
		store:     s,
		topK:      topK,
		history:   make(map[string][]llm.Message),
	}
}

func (a *Agent) Run(ctx context.Context) error {
	a.connector.SetMessageHandler(a.handleMessage)
	if err := a.connector.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return a.connector.Stop(context.Background())
}

func (a *Agent) handleMessage(ctx context.Context, msg connector.Message) {
	userKey := msg.Connector + ":" + msg.UserID

	// Inject user identity into context so tools can access it.
	ctx = context.WithValue(ctx, memory.UserKey{}, memory.UserInfo{
		Connector: msg.Connector,
		UserID:    msg.UserID,
	})

	// Fetch relevant long-term memories.
	systemPrompt := a.buildSystemPrompt(ctx, msg)

	// Retrieve and append to per-user short-term history.
	a.historyMu.Lock()
	hist := append(a.history[userKey], llm.Message{Role: "user", Text: msg.Text})
	a.historyMu.Unlock()

	tools := a.registry.Tools()

	for round := 0; round < maxToolRounds; round++ {
		resp, err := a.llm.ChatWithSystem(ctx, systemPrompt, hist, tools)
		if err != nil {
			log.Printf("llm error: %v", err)
			return
		}

		if resp.ToolCall == nil {
			// Final answer — persist history and send reply.
			a.historyMu.Lock()
			a.history[userKey] = append(hist, llm.Message{Role: "model", Text: resp.Text})
			a.historyMu.Unlock()

			if err := a.connector.Send(ctx, msg.ChatID, resp.Text); err != nil {
				log.Printf("send error: %v", err)
			}
			return
		}

		tc := resp.ToolCall
		log.Printf("tool call: %s %v", tc.Name, tc.Args)

		result, err := a.registry.Dispatch(ctx, tc.Name, tc.Args)
		if err != nil {
			log.Printf("tool error: %v", err)
			result = "error: " + err.Error()
		}

		log.Printf("tool result: %s → %s", tc.Name, truncate(result, 80))

		hist = append(hist,
			llm.Message{Role: "model", ToolCall: tc},
			llm.Message{ToolResult: &llm.ToolResult{Name: tc.Name, Result: result}},
		)
	}

	log.Printf("agent: reached max tool rounds (%d)", maxToolRounds)
}

func (a *Agent) buildSystemPrompt(ctx context.Context, msg connector.Message) string {
	base := "You are a helpful assistant."

	if a.embedder == nil || a.store == nil {
		return base
	}

	embedding, err := a.embedder.Embed(ctx, msg.Text)
	if err != nil {
		log.Printf("embed error: %v", err)
		return base
	}

	facts, err := a.store.SearchMemories(ctx, msg.Connector, msg.UserID, embedding, a.topK)
	if err != nil {
		log.Printf("memory search error: %v", err)
		return base
	}

	if len(facts) == 0 {
		return base
	}

	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteString("\n\nLong-term memory about this user:\n")
	for _, f := range facts {
		fmt.Fprintf(&sb, "- %s\n", f)
	}
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
