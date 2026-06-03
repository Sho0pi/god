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

	Close() error
}
