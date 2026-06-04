package store

import (
	"context"
	"testing"
)

// recStore records the (connector, userID) each method received, and lets a
// test program a fixed identity resolution.
type recStore struct {
	resolveTo  map[string][2]string // "conn:user" → {canonConn, canonUser}
	gotSoul    [2]string
	gotMemory  [2]string
	gotAllow   string
	linkCalled bool
}

func (r *recStore) ResolveIdentity(_ context.Context, c, u string) (string, string, error) {
	if v, ok := r.resolveTo[c+":"+u]; ok {
		return v[0], v[1], nil
	}
	return c, u, nil
}
func (r *recStore) Link(_ context.Context, _, _, _, _ string) error { r.linkCalled = true; return nil }
func (r *recStore) Unlink(_ context.Context, _, _ string) error     { return nil }

func (r *recStore) AssignSoul(_ context.Context, c, u, _ string) error {
	r.gotSoul = [2]string{c, u}
	return nil
}
func (r *recStore) GetSoul(_ context.Context, c, u string) (string, error) {
	r.gotSoul = [2]string{c, u}
	return "", nil
}
func (r *recStore) DeleteSoul(context.Context, string, string) error         { return nil }
func (r *recStore) AssignRole(context.Context, string, string, string) error { return nil }
func (r *recStore) GetRole(context.Context, string, string) (string, error)  { return "", nil }
func (r *recStore) DeleteRole(context.Context, string, string) error         { return nil }
func (r *recStore) SaveMemory(_ context.Context, c, u, _ string, _ []float32) error {
	r.gotMemory = [2]string{c, u}
	return nil
}
func (r *recStore) SearchMemories(_ context.Context, c, u string, _ []float32, _ int) ([]string, error) {
	r.gotMemory = [2]string{c, u}
	return nil, nil
}
func (r *recStore) DeleteMemories(context.Context, string, string) error { return nil }
func (r *recStore) AddAllow(_ context.Context, c, _ string) error        { r.gotAllow = c; return nil }
func (r *recStore) RemoveAllow(context.Context, string, string) error    { return nil }
func (r *recStore) ListAllow(context.Context, string) ([]string, error)  { return nil, nil }
func (r *recStore) Close() error                                         { return nil }

func TestCanonicalResolvesProfileOps(t *testing.T) {
	inner := &recStore{resolveTo: map[string][2]string{
		"telegram:7474": {"whatsapp", "972"}, // telegram identity is linked to whatsapp hub
	}}
	c := Canonical(inner)
	ctx := context.Background()

	// Soul + memory ops on the linked identity should hit the hub.
	_, _ = c.GetSoul(ctx, "telegram", "7474")
	if inner.gotSoul != [2]string{"whatsapp", "972"} {
		t.Errorf("GetSoul resolved to %v, want whatsapp/972", inner.gotSoul)
	}
	_ = c.SaveMemory(ctx, "telegram", "7474", "fact", nil)
	if inner.gotMemory != [2]string{"whatsapp", "972"} {
		t.Errorf("SaveMemory resolved to %v, want whatsapp/972", inner.gotMemory)
	}
}

func TestCanonicalUnlinkedPassesThrough(t *testing.T) {
	inner := &recStore{} // no links
	c := Canonical(inner)
	_, _ = c.GetSoul(context.Background(), "telegram", "555")
	if inner.gotSoul != [2]string{"telegram", "555"} {
		t.Errorf("unlinked identity should pass through, got %v", inner.gotSoul)
	}
}

func TestCanonicalAllowNotResolved(t *testing.T) {
	inner := &recStore{resolveTo: map[string][2]string{"telegram:7474": {"whatsapp", "972"}}}
	c := Canonical(inner)
	// Allow is per-connector, not per identity — must not be remapped.
	_ = c.AddAllow(context.Background(), "telegram", "123")
	if inner.gotAllow != "telegram" {
		t.Errorf("AddAllow connector = %q, want telegram (no resolution)", inner.gotAllow)
	}
}
