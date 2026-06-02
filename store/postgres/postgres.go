package postgres

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	pgxvector "github.com/pgvector/pgvector-go/pgx"
)

//go:embed schema.sql
var schema string

type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse db url: %w", err)
	}

	// Register pgvector types for every new connection in the pool.
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvector.RegisterTypes(ctx, conn)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open db pool: %w", err)
	}

	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}

	return &Store{pool: pool}, nil
}

func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

// AssignSoul upserts the soul for a (connector, user_id) pair.
func (s *Store) AssignSoul(ctx context.Context, connector, userID, soulName string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO soul_assignments (connector, user_id, soul_name, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (connector, user_id)
		DO UPDATE SET soul_name = EXCLUDED.soul_name, updated_at = NOW()
	`, connector, userID, soulName)
	return err
}

// GetSoul returns the soul name for a user, or "" if not assigned.
func (s *Store) GetSoul(ctx context.Context, connector, userID string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx,
		`SELECT soul_name FROM soul_assignments WHERE connector=$1 AND user_id=$2`,
		connector, userID,
	).Scan(&name)
	if err != nil {
		// pgx returns pgx.ErrNoRows when not found — return empty string, not an error.
		return "", nil
	}
	return name, nil
}

// SaveMemory persists a fact with its vector embedding.
func (s *Store) SaveMemory(ctx context.Context, connector, userID, fact string, embedding []float32) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO memories (connector, user_id, fact, embedding) VALUES ($1, $2, $3, $4)`,
		connector, userID, fact, pgvector.NewVector(embedding),
	)
	return err
}

// SearchMemories returns the top-K facts closest to the query embedding.
func (s *Store) SearchMemories(ctx context.Context, connector, userID string, queryEmbedding []float32, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT fact
		FROM memories
		WHERE connector = $1 AND user_id = $2
		ORDER BY embedding <=> $3
		LIMIT $4
	`, connector, userID, pgvector.NewVector(queryEmbedding), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var facts []string
	for rows.Next() {
		var fact string
		if err := rows.Scan(&fact); err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	return facts, rows.Err()
}
