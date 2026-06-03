package store

import "context"

// Store is the persistence layer for soul assignments and long-term memories.
type Store interface {
	// Soul assignments
	AssignSoul(ctx context.Context, connector, userID, soulName string) error
	GetSoul(ctx context.Context, connector, userID string) (string, error)
	DeleteSoul(ctx context.Context, connector, userID string) error

	// Role assignments
	AssignRole(ctx context.Context, connector, userID, roleName string) error
	GetRole(ctx context.Context, connector, userID string) (string, error)
	DeleteRole(ctx context.Context, connector, userID string) error

	// Long-term memory
	SaveMemory(ctx context.Context, connector, userID, fact string, embedding []float32) error
	SearchMemories(ctx context.Context, connector, userID string, queryEmbedding []float32, limit int) ([]string, error)
	DeleteMemories(ctx context.Context, connector, userID string) error

	// Allow list (per connector). Numbers are stored as caller-supplied strings;
	// the connector is responsible for normalization.
	AddAllow(ctx context.Context, connector, number string) error
	RemoveAllow(ctx context.Context, connector, number string) error
	ListAllow(ctx context.Context, connector string) ([]string, error)

	Close() error
}
