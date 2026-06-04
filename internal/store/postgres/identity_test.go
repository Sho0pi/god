package postgres_test

import (
	"testing"

	"github.com/sho0pi/god/internal/store/postgres"
)

func TestPostgres_LinkAndResolve(t *testing.T) {
	url, ctx := requireDB(t)
	s, err := postgres.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.Close()

	// Unique identities per run to avoid cross-test collisions.
	hubConn, hubUser := "whatsapp", "hub_"+t.Name()
	satConn, satUser := "telegram", "sat_"+t.Name()

	// Hub has a soul + a memory; satellite has its own soul + memory.
	if err := s.AssignSoul(ctx, hubConn, hubUser, "caveman"); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignSoul(ctx, satConn, satUser, "human"); err != nil {
		t.Fatal(err)
	}
	emb := make([]float32, 3072)
	_ = s.SaveMemory(ctx, satConn, satUser, "satellite fact", emb)

	// Unlinked: resolves to self.
	c, u, _ := s.ResolveIdentity(ctx, satConn, satUser)
	if c != satConn || u != satUser {
		t.Fatalf("unlinked resolve = %s/%s, want self", c, u)
	}

	// Link satellite → hub.
	if err := s.Link(ctx, satConn, satUser, hubConn, hubUser); err != nil {
		t.Fatalf("Link: %v", err)
	}

	// Now resolves to the hub.
	c, u, _ = s.ResolveIdentity(ctx, satConn, satUser)
	if c != hubConn || u != hubUser {
		t.Errorf("linked resolve = %s/%s, want hub %s/%s", c, u, hubConn, hubUser)
	}

	// Satellite's soul row was dropped (it adopts the hub's).
	if got, _ := s.GetSoul(ctx, satConn, satUser); got != "" {
		t.Errorf("satellite soul should be dropped, got %q", got)
	}
	// Satellite's memory was merged into the hub.
	facts, _ := s.SearchMemories(ctx, hubConn, hubUser, emb, 10)
	found := false
	for _, f := range facts {
		if f == "satellite fact" {
			found = true
		}
	}
	if !found {
		t.Errorf("satellite memory not merged into hub; got %v", facts)
	}

	// Double-link is rejected.
	if err := s.Link(ctx, satConn, satUser, hubConn, hubUser); err == nil {
		t.Error("expected double-link to be rejected")
	}

	// Unlink restores self-resolution.
	if err := s.Unlink(ctx, satConn, satUser); err != nil {
		t.Fatalf("Unlink: %v", err)
	}
	c, u, _ = s.ResolveIdentity(ctx, satConn, satUser)
	if c != satConn || u != satUser {
		t.Errorf("after unlink resolve = %s/%s, want self", c, u)
	}
}

func TestPostgres_LinkRejectsSelfAndSameConnector(t *testing.T) {
	url, ctx := requireDB(t)
	s, err := postgres.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.Close()

	// Self-link rejected.
	if err := s.Link(ctx, "whatsapp", "a_"+t.Name(), "whatsapp", "a_"+t.Name()); err == nil {
		t.Error("self-link should be rejected")
	}

	// Linking a second identity on the hub's own connector is rejected.
	hubUser := "hub2_" + t.Name()
	if err := s.Link(ctx, "whatsapp", "other_"+t.Name(), "whatsapp", hubUser); err == nil {
		t.Error("same-connector link should be rejected")
	}
}
