package agent

import (
	"context"
	"fmt"
	"time"
)

// linkCodeTTL is how long a generated link code stays redeemable.
const linkCodeTTL = 10 * time.Minute

// linkCode is a one-time code, shown on an existing (hub) chat, that another
// chat redeems to link itself to that account.
type linkCode struct {
	connector string
	userID    string
	expires   time.Time
}

// generateLinkCode mints a code for the given (hub) identity and stores it with
// a TTL. The user shows it on the chat they want to join.
func (a *Agent) generateLinkCode(connector, userID string) string {
	code := newApprovalID() // short random hex, reused from the approval gate
	a.linkMu.Lock()
	a.linkCodes[code] = linkCode{connector: connector, userID: userID, expires: time.Now().Add(linkCodeTTL)}
	a.linkMu.Unlock()
	return code
}

// redeemLinkCode links the calling (satellite) identity to the hub identity that
// generated code, then clears the satellite's short-term history so it
// re-resolves to the shared profile. Returns a human label for the hub.
func (a *Agent) redeemLinkCode(ctx context.Context, code, satConnector, satUserID string) (string, error) {
	a.linkMu.Lock()
	lc, ok := a.linkCodes[code]
	if ok {
		delete(a.linkCodes, code) // one-time use
	}
	a.linkMu.Unlock()

	if !ok || time.Now().After(lc.expires) {
		return "", fmt.Errorf("invalid or expired code")
	}
	if a.store == nil {
		return "", fmt.Errorf("linking unavailable (no store)")
	}
	if err := a.store.Link(ctx, satConnector, satUserID, lc.connector, lc.userID); err != nil {
		return "", err
	}
	a.clearUserHistory(satConnector + ":" + satUserID)
	return lc.connector + ":" + lc.userID, nil
}
