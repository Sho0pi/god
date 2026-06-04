package store

import "context"

// SoulStore persists per-user soul assignments.
type SoulStore interface {
	AssignSoul(ctx context.Context, connector, userID, soulName string) error
	GetSoul(ctx context.Context, connector, userID string) (string, error)
	DeleteSoul(ctx context.Context, connector, userID string) error
}

// RoleStore persists per-user role assignments.
type RoleStore interface {
	AssignRole(ctx context.Context, connector, userID, roleName string) error
	GetRole(ctx context.Context, connector, userID string) (string, error)
	DeleteRole(ctx context.Context, connector, userID string) error
}

// MemoryStore persists long-term memories as embedded facts.
type MemoryStore interface {
	SaveMemory(ctx context.Context, connector, userID, fact string, embedding []float32) error
	SearchMemories(ctx context.Context, connector, userID string, queryEmbedding []float32, limit int) ([]string, error)
	DeleteMemories(ctx context.Context, connector, userID string) error
}

// AllowStore persists the per-connector allow list. Numbers are stored as
// caller-supplied strings; the connector is responsible for normalization.
type AllowStore interface {
	AddAllow(ctx context.Context, connector, number string) error
	RemoveAllow(ctx context.Context, connector, number string) error
	ListAllow(ctx context.Context, connector string) ([]string, error)
}

// Store is the full persistence layer. Consumers that need only one concern
// should depend on the narrower interface above (SoulStore, MemoryStore, ...).
type Store interface {
	SoulStore
	RoleStore
	MemoryStore
	AllowStore
	IdentityStore

	Close() error
}

// IdentityStore links chat identities across connectors so one person using e.g.
// WhatsApp + Telegram shares a single profile (soul, role, memory).
type IdentityStore interface {
	// ResolveIdentity returns the canonical (hub) identity for the given
	// identity, or the identity itself when it is not linked.
	ResolveIdentity(ctx context.Context, connector, userID string) (canonConnector, canonUserID string, err error)
	// Link points the satellite identity at the hub's canonical account: the
	// satellite's memories are merged into the hub, its soul/role rows are
	// dropped (it adopts the hub's), and the link is recorded. It rejects
	// self-links, already-linked satellites, and a second identity on a
	// connector the account already uses.
	Link(ctx context.Context, satConnector, satUserID, hubConnector, hubUserID string) error
	// Unlink detaches the identity from its hub (no-op if not linked).
	Unlink(ctx context.Context, connector, userID string) error
}
