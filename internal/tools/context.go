package tools

import "context"

// UserKey is the context key under which the agent stores the requesting user's
// identity, so tools can attribute their actions (e.g. remember writes to the
// right user's long-term memory).
type UserKey struct{}

// UserInfo identifies the user a tool call is acting on behalf of.
type UserInfo struct {
	Connector string
	UserID    string
}

// UserFrom extracts the UserInfo the agent placed in ctx. ok is false when no
// user identity is present.
func UserFrom(ctx context.Context) (UserInfo, bool) {
	u, ok := ctx.Value(UserKey{}).(UserInfo)
	return u, ok && u.Connector != ""
}
