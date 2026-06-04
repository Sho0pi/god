package whatsapp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionExists(t *testing.T) {
	dir := t.TempDir()
	if SessionExists(dir) {
		t.Error("empty dir should report no session")
	}
	if err := os.WriteFile(filepath.Join(dir, dbName), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !SessionExists(dir) {
		t.Error("store.db present should report a session")
	}
}

// Pair with reset=true deletes an existing store.db before attempting login.
// Reset deletion runs synchronously at the top of Pair, before any
// context-checked step, so a pre-cancelled context lets us assert the deletion
// happened without performing a real network login.
func TestPairResetDeletesStore(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, dbName)
	if err := os.WriteFile(db, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pair will bail at the first ctx-checked step (PRAGMA exec)

	_ = Pair(ctx, dir, true, nil)

	if b, err := os.ReadFile(db); err == nil && string(b) == "stale" {
		t.Error("reset did not remove the stale store.db")
	}
}
