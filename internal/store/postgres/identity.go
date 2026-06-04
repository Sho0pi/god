package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ResolveIdentity returns the canonical (hub) identity for (connector, userID),
// or the identity itself when it is not linked.
func (s *Store) ResolveIdentity(ctx context.Context, connector, userID string) (string, string, error) {
	var canonConn, canonUser string
	err := s.pool.QueryRow(ctx,
		`SELECT canon_connector, canon_user_id FROM identity_links WHERE connector=$1 AND user_id=$2`,
		connector, userID).Scan(&canonConn, &canonUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return connector, userID, nil
	}
	if err != nil {
		return "", "", fmt.Errorf("resolve identity: %w", err)
	}
	return canonConn, canonUser, nil
}

// Link points the satellite identity at the hub's canonical account. See the
// IdentityStore docs for the merge semantics and the rejected cases.
func (s *Store) Link(ctx context.Context, satConnector, satUserID, hubConnector, hubUserID string) error {
	// Resolve the hub to its own canonical, so linking to an already-linked
	// identity attaches to the real account (no chains).
	canonConn, canonUser, err := s.ResolveIdentity(ctx, hubConnector, hubUserID)
	if err != nil {
		return err
	}

	if satConnector == canonConn && satUserID == canonUser {
		return errors.New("cannot link an identity to itself")
	}
	if satConnector == canonConn {
		return fmt.Errorf("that account already uses %s — link from a different app", satConnector)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Reject if the satellite is itself already a hub for someone, or already linked.
	var n int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM identity_links WHERE (connector=$1 AND user_id=$2)
		    OR (canon_connector=$1 AND canon_user_id=$2)`,
		satConnector, satUserID).Scan(&n); err != nil {
		return fmt.Errorf("check satellite: %w", err)
	}
	if n > 0 {
		return errors.New("this chat is already linked — /unlink first")
	}

	// Reject a second identity on the satellite's connector for this account.
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM identity_links WHERE canon_connector=$1 AND canon_user_id=$2 AND connector=$3`,
		canonConn, canonUser, satConnector).Scan(&n); err != nil {
		return fmt.Errorf("check connector slot: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("that account already has a linked %s identity", satConnector)
	}

	// Merge the satellite's memories into the hub (union), then drop its
	// soul/role (it adopts the hub's), then record the link.
	if _, err := tx.Exec(ctx,
		`UPDATE memories SET connector=$1, user_id=$2 WHERE connector=$3 AND user_id=$4`,
		canonConn, canonUser, satConnector, satUserID); err != nil {
		return fmt.Errorf("merge memories: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM soul_assignments WHERE connector=$1 AND user_id=$2`, satConnector, satUserID); err != nil {
		return fmt.Errorf("drop soul: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM role_assignments WHERE connector=$1 AND user_id=$2`, satConnector, satUserID); err != nil {
		return fmt.Errorf("drop role: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO identity_links (connector, user_id, canon_connector, canon_user_id)
		 VALUES ($1, $2, $3, $4)`,
		satConnector, satUserID, canonConn, canonUser); err != nil {
		return fmt.Errorf("insert link: %w", err)
	}

	return tx.Commit(ctx)
}

// Unlink detaches the identity from its hub (no-op if not linked).
func (s *Store) Unlink(ctx context.Context, connector, userID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM identity_links WHERE connector=$1 AND user_id=$2`, connector, userID)
	if err != nil {
		return fmt.Errorf("unlink: %w", err)
	}
	return nil
}
