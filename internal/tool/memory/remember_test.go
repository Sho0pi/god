package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sho0pi/god/internal/tool/memory"
)

// --- mocks ---

type mockEmbedder struct {
	result []float32
	err    error
	calls  []string
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	m.calls = append(m.calls, text)
	return m.result, m.err
}

type mockStore struct {
	saved []savedEntry
	err   error
}

type savedEntry struct {
	connector string
	userID    string
	fact      string
	embedding []float32
}

func (m *mockStore) SaveMemory(_ context.Context, connector, userID, fact string, embedding []float32) error {
	m.saved = append(m.saved, savedEntry{connector, userID, fact, embedding})
	return m.err
}
func (m *mockStore) SearchMemories(_ context.Context, _, _ string, _ []float32, _ int) ([]string, error) {
	return nil, nil
}
func (m *mockStore) AssignSoul(_ context.Context, _, _, _ string) error      { return nil }
func (m *mockStore) GetSoul(_ context.Context, _, _ string) (string, error)  { return "", nil }
func (m *mockStore) DeleteSoul(_ context.Context, _, _ string) error         { return nil }
func (m *mockStore) AssignRole(_ context.Context, _, _, _ string) error      { return nil }
func (m *mockStore) GetRole(_ context.Context, _, _ string) (string, error)  { return "", nil }
func (m *mockStore) DeleteRole(_ context.Context, _, _ string) error         { return nil }
func (m *mockStore) DeleteMemories(_ context.Context, _, _ string) error     { return nil }
func (m *mockStore) AddAllow(_ context.Context, _, _ string) error           { return nil }
func (m *mockStore) RemoveAllow(_ context.Context, _, _ string) error        { return nil }
func (m *mockStore) ListAllow(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (m *mockStore) Close() error                                            { return nil }

// --- helpers ---

func ctxWithUser(connector, userID string) context.Context {
	return context.WithValue(context.Background(), memory.UserKey{}, memory.UserInfo{
		Connector: connector,
		UserID:    userID,
	})
}

// --- tests ---

func TestRemember_SavesFact(t *testing.T) {
	emb := &mockEmbedder{result: []float32{0.1, 0.2, 0.3}}
	store := &mockStore{}
	tool := memory.NewRememberTool(emb, store)

	ctx := ctxWithUser("whatsapp", "972526777236")
	result, err := tool.Execute(ctx, map[string]any{"fact": "user is a Go developer"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.saved) != 1 {
		t.Fatalf("expected 1 saved fact, got %d", len(store.saved))
	}

	entry := store.saved[0]
	if entry.connector != "whatsapp" {
		t.Errorf("connector = %q, want whatsapp", entry.connector)
	}
	if entry.userID != "972526777236" {
		t.Errorf("userID = %q, want 972526777236", entry.userID)
	}
	if entry.fact != "user is a Go developer" {
		t.Errorf("fact = %q, want 'user is a Go developer'", entry.fact)
	}
	if len(entry.embedding) != 3 {
		t.Errorf("embedding len = %d, want 3", len(entry.embedding))
	}
	t.Logf("result: %s", result)
}

func TestRemember_MissingFact(t *testing.T) {
	tool := memory.NewRememberTool(&mockEmbedder{}, &mockStore{})
	_, err := tool.Execute(ctxWithUser("cli", "local"), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing fact, got nil")
	}
}

func TestRemember_NoUserContext(t *testing.T) {
	tool := memory.NewRememberTool(&mockEmbedder{result: []float32{0.1}}, &mockStore{})
	_, err := tool.Execute(context.Background(), map[string]any{"fact": "something"})
	if err == nil {
		t.Fatal("expected error when no user context, got nil")
	}
}

func TestRemember_EmbedError(t *testing.T) {
	emb := &mockEmbedder{err: errors.New("embed failed")}
	tool := memory.NewRememberTool(emb, &mockStore{})
	_, err := tool.Execute(ctxWithUser("cli", "local"), map[string]any{"fact": "test"})
	if err == nil {
		t.Fatal("expected error when embed fails, got nil")
	}
}

func TestRemember_StoreError(t *testing.T) {
	emb := &mockEmbedder{result: []float32{0.1}}
	store := &mockStore{err: errors.New("db down")}
	tool := memory.NewRememberTool(emb, store)
	_, err := tool.Execute(ctxWithUser("cli", "local"), map[string]any{"fact": "test"})
	if err == nil {
		t.Fatal("expected error when store fails, got nil")
	}
}

func TestRemember_Schema(t *testing.T) {
	tool := memory.NewRememberTool(&mockEmbedder{}, &mockStore{})
	schema := tool.Schema()
	if _, ok := schema.Properties["fact"]; !ok {
		t.Error("schema missing 'fact' property")
	}
	if len(schema.Required) == 0 || schema.Required[0] != "fact" {
		t.Error("'fact' should be required")
	}
}
