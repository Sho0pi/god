package store

import "context"

// Store is the persistence layer for soul assignments and long-term memories.
type Store interface {
	// Soul assignments
	AssignSoul(ctx context.Context, connector, userID, soulName string) error
	GetSoul(ctx context.Context, connector, userID string) (string, error)

	// Long-term memory
	SaveMemory(ctx context.Context, connector, userID, fact string, embedding []float32) error
	SearchMemories(ctx context.Context, connector, userID string, queryEmbedding []float32, limit int) ([]string, error)

	Close() error
}
