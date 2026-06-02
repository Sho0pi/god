package connector

import "context"

type Message struct {
	ChatID   string
	SenderID string
	Text     string
}

type Connector interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Send(ctx context.Context, chatID, text string) error
	SetMessageHandler(handler func(ctx context.Context, msg Message))
}
