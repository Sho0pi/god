package agent

import (
	"context"
	"log"

	"github.com/sho0pi/god/connector"
	"github.com/sho0pi/god/llm"
	"github.com/sho0pi/god/tool"
)

const maxToolRounds = 10

type Agent struct {
	connector connector.Connector
	llm       llm.LLM
	registry  *tool.Registry
}

func New(c connector.Connector, l llm.LLM, r *tool.Registry) *Agent {
	return &Agent{connector: c, llm: l, registry: r}
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
	history := []llm.Message{{Role: "user", Text: msg.Text}}
	tools := a.registry.Tools()

	for round := 0; round < maxToolRounds; round++ {
		resp, err := a.llm.Chat(ctx, history, tools)
		if err != nil {
			log.Printf("llm error: %v", err)
			return
		}

		if resp.ToolCall == nil {
			// Final text answer — send to user.
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

		history = append(history,
			llm.Message{Role: "model", ToolCall: tc},
			llm.Message{ToolResult: &llm.ToolResult{Name: tc.Name, Result: result}},
		)
	}

	log.Printf("agent: reached max tool rounds (%d), giving up", maxToolRounds)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
