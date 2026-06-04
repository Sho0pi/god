// Package socket implements god's control channel: a Unix-domain-socket server
// that runs as a connector inside the gateway, and a thin client used by
// `god cli`. Each client connection is one chat session; frames are
// newline-delimited JSON so the wire format stays trivially debuggable.
package socket

// connectorName is the connector identity reported for socket sessions. It
// reuses "cli" so existing connectors.cli soul/role defaults and allow config
// keep applying to socket clients unchanged.
const connectorName = "cli"

// clientFrame is sent client → server: one chat message.
type clientFrame struct {
	User string `json:"user,omitempty"` // identity to act as (default "local")
	Text string `json:"text"`
}

// serverFrame is sent server → client: one reply.
type serverFrame struct {
	Text string `json:"text"`
}
