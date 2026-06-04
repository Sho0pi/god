package store

import "context"

// canonicalStore wraps a Store so that soul, role, and memory operations act on
// the caller's canonical (linked) identity instead of the raw (connector,
// userID). This lets one person who linked WhatsApp + Telegram share a single
// profile and memory, with zero changes at the call sites — they keep passing
// the raw identity, and the resolution happens here.
//
// Allow-list and identity operations pass through unchanged: allow lists are
// per-connector (not per identity), and identity ops are how links are managed.
type canonicalStore struct {
	Store
}

// Canonical returns a Store that resolves each identity to its canonical hub
// before soul/role/memory operations.
func Canonical(inner Store) Store {
	return &canonicalStore{Store: inner}
}

func (c *canonicalStore) resolve(ctx context.Context, connector, userID string) (string, string) {
	cc, cu, err := c.ResolveIdentity(ctx, connector, userID) // not overridden; uses embedded Store

	if err != nil {
		// On lookup failure, fall back to the raw identity rather than losing
		// access to the user's own data.
		return connector, userID
	}
	return cc, cu
}

func (c *canonicalStore) AssignSoul(ctx context.Context, connector, userID, soulName string) error {
	cc, cu := c.resolve(ctx, connector, userID)
	return c.Store.AssignSoul(ctx, cc, cu, soulName)
}

func (c *canonicalStore) GetSoul(ctx context.Context, connector, userID string) (string, error) {
	cc, cu := c.resolve(ctx, connector, userID)
	return c.Store.GetSoul(ctx, cc, cu)
}

func (c *canonicalStore) DeleteSoul(ctx context.Context, connector, userID string) error {
	cc, cu := c.resolve(ctx, connector, userID)
	return c.Store.DeleteSoul(ctx, cc, cu)
}

func (c *canonicalStore) AssignRole(ctx context.Context, connector, userID, roleName string) error {
	cc, cu := c.resolve(ctx, connector, userID)
	return c.Store.AssignRole(ctx, cc, cu, roleName)
}

func (c *canonicalStore) GetRole(ctx context.Context, connector, userID string) (string, error) {
	cc, cu := c.resolve(ctx, connector, userID)
	return c.Store.GetRole(ctx, cc, cu)
}

func (c *canonicalStore) DeleteRole(ctx context.Context, connector, userID string) error {
	cc, cu := c.resolve(ctx, connector, userID)
	return c.Store.DeleteRole(ctx, cc, cu)
}

func (c *canonicalStore) SaveMemory(ctx context.Context, connector, userID, fact string, embedding []float32) error {
	cc, cu := c.resolve(ctx, connector, userID)
	return c.Store.SaveMemory(ctx, cc, cu, fact, embedding)
}

func (c *canonicalStore) SearchMemories(ctx context.Context, connector, userID string, queryEmbedding []float32, limit int) ([]string, error) {
	cc, cu := c.resolve(ctx, connector, userID)
	return c.Store.SearchMemories(ctx, cc, cu, queryEmbedding, limit)
}

func (c *canonicalStore) DeleteMemories(ctx context.Context, connector, userID string) error {
	cc, cu := c.resolve(ctx, connector, userID)
	return c.Store.DeleteMemories(ctx, cc, cu)
}
