package connector

import "context"

type Message struct {
	Connector string // connector name, e.g. "whatsapp", "cli"
	UserID    string // stable user identifier within the connector
	ChatID    string // where to send the reply (may differ from UserID in groups)
	SenderID  string // raw sender JID / platform ID
	Text      string
}

type Connector interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Send(ctx context.Context, chatID, text string) error
	SetMessageHandler(handler func(ctx context.Context, msg Message))
}
