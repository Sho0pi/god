package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/sho0pi/god/store/postgres"
)

// Integration tests — skipped when DATABASE_URL is not set.

func requireDB(t *testing.T) (string, context.Context) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set — skipping postgres integration tests")
	}
	return url, context.Background()
}

func TestPostgres_SoulAssignment(t *testing.T) {
	url, ctx := requireDB(t)

	s, err := postgres.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.Close()

	connector, userID := "test", "user_soul_"+t.Name()

	// Assign a soul.
	if err := s.AssignSoul(ctx, connector, userID, "vibe-coder"); err != nil {
		t.Fatalf("AssignSoul: %v", err)
	}

	got, err := s.GetSoul(ctx, connector, userID)
	if err != nil {
		t.Fatalf("GetSoul: %v", err)
	}
	if got != "vibe-coder" {
		t.Errorf("soul = %q, want 'vibe-coder'", got)
	}

	// Re-assign and verify upsert.
	if err := s.AssignSoul(ctx, connector, userID, "assistant"); err != nil {
		t.Fatalf("re-assign: %v", err)
	}
	got, _ = s.GetSoul(ctx, connector, userID)
	if got != "assistant" {
		t.Errorf("after re-assign soul = %q, want 'assistant'", got)
	}
}

func TestPostgres_GetSoul_NotFound(t *testing.T) {
	url, ctx := requireDB(t)
	s, err := postgres.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.Close()

	soul, err := s.GetSoul(ctx, "test", "nonexistent_user_xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if soul != "" {
		t.Errorf("expected empty soul for unknown user, got %q", soul)
	}
}

func TestPostgres_SaveAndSearchMemory(t *testing.T) {
	url, ctx := requireDB(t)
	s, err := postgres.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.Close()

	connector, userID := "test", "user_mem_"+t.Name()

	// Use a simple 3-dim embedding (pgvector needs consistent dims — 768 in prod,
	// but we override the schema dimension for test by using 3-dim vectors directly).
	// Actually our schema hardcodes vector(768), so we need 768-dim here.
	embedding := make([]float32, 768)
	embedding[0] = 1.0 // distinguishable

	if err := s.SaveMemory(ctx, connector, userID, "user likes Go", embedding); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	// Query with the same embedding — should return itself as the closest.
	facts, err := s.SearchMemories(ctx, connector, userID, embedding, 5)
	if err != nil {
		t.Fatalf("SearchMemories: %v", err)
	}
	if len(facts) == 0 {
		t.Fatal("expected at least 1 memory, got 0")
	}
	if facts[0] != "user likes Go" {
		t.Errorf("fact = %q, want 'user likes Go'", facts[0])
	}
}

func TestPostgres_SearchMemory_WrongUser(t *testing.T) {
	url, ctx := requireDB(t)
	s, err := postgres.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.Close()

	embedding := make([]float32, 768)
	// Save under user A, search under user B — should return nothing.
	_ = s.SaveMemory(ctx, "test", "userA_isolation_"+t.Name(), "secret", embedding)

	facts, err := s.SearchMemories(ctx, "test", "userB_isolation_"+t.Name(), embedding, 5)
	if err != nil {
		t.Fatalf("SearchMemories: %v", err)
	}
	if len(facts) != 0 {
		t.Errorf("expected 0 facts for wrong user, got %d", len(facts))
	}
}
