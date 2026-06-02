package agent

import (
	"context"
	"log"

	"github.com/sho0pi/god/connector"
	"github.com/sho0pi/god/llm"
)

type Agent struct {
	connector connector.Connector
	llm       llm.LLM
}

func New(c connector.Connector, l llm.LLM) *Agent {
	return &Agent{connector: c, llm: l}
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
	reply, err := a.llm.Chat(ctx, msg.Text)
	if err != nil {
		log.Printf("llm error: %v", err)
		return
	}
	if err := a.connector.Send(ctx, msg.ChatID, reply); err != nil {
		log.Printf("send error: %v", err)
	}
}
