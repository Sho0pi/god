package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/sho0pi/god/llm"
	"github.com/sho0pi/god/tool"
	"github.com/sho0pi/god/tool/memory"
)

// approvalTTL is how long a parked approval waits before it is auto-discarded.
const approvalTTL = 10 * time.Minute

// pendingApproval is a tool call that god has paused, awaiting an admin
// /approve or /deny. It carries everything needed to resume the tool loop.
type pendingApproval struct {
	id           string
	connector    string
	userID       string
	chatID       string
	toolCall     *llm.ToolCall
	hist         []llm.Message // includes the model's tool-call turn
	systemPrompt string
	tools        []tool.Tool
	llm          llm.LLM
	expires      time.Time
}

// needsApproval reports whether a tool requires admin approval before running,
// per the live config (tools.approval list).
func (a *Agent) needsApproval(toolName string) bool {
	for _, n := range a.liveConfig().Tools.Approval {
		if n == toolName {
			return true
		}
	}
	return false
}

func (a *Agent) setPending(userKey string, p *pendingApproval) {
	a.pendingMu.Lock()
	a.pending[userKey] = p
	a.pendingMu.Unlock()
}

// getPending returns the live pending approval for a user, or nil if none or
// expired (expired entries are cleared).
func (a *Agent) getPending(userKey string) *pendingApproval {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	p, ok := a.pending[userKey]
	if !ok {
		return nil
	}
	if time.Now().After(p.expires) {
		delete(a.pending, userKey)
		return nil
	}
	return p
}

// takePending removes and returns the pending approval, or nil if none/expired.
func (a *Agent) takePending(userKey string) *pendingApproval {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	p, ok := a.pending[userKey]
	if !ok {
		return nil
	}
	delete(a.pending, userKey)
	if time.Now().After(p.expires) {
		return nil
	}
	return p
}

// resumeApproval runs (or rejects) a previously parked tool call and continues
// the tool loop. Acquires the per-user lock itself.
func (a *Agent) resumeApproval(ctx context.Context, userKey, chatID string, approve bool, id string) {
	unlock := a.lockUser(userKey)
	defer unlock()

	p := a.takePending(userKey)
	if p == nil {
		a.sendOrLog(ctx, chatID, "No pending approval found (it may have expired).")
		return
	}
	if p.id != id {
		a.setPending(userKey, p) // not ours — put it back
		a.sendOrLog(ctx, chatID, fmt.Sprintf("Approval id mismatch — the pending action is %s.", p.id))
		return
	}

	// Restore user identity for tools that read it from context.
	ctx = context.WithValue(ctx, memory.UserKey{}, memory.UserInfo{
		Connector: p.connector,
		UserID:    p.userID,
	})

	var result string
	if approve {
		r, err := a.registry.Dispatch(ctx, p.toolCall.Name, p.toolCall.Args)
		if err != nil {
			result = "error: " + err.Error()
		} else {
			result = r
		}
		log.Printf("approval %s APPROVED: %s → %s", id, p.toolCall.Name, truncate(result, 80))
	} else {
		result = "The user denied this action. Do not perform it; tell them it was cancelled."
		log.Printf("approval %s DENIED: %s", id, p.toolCall.Name)
	}

	hist := append(p.hist, llm.Message{ToolResult: &llm.ToolResult{
		Name:             p.toolCall.Name,
		Result:           result,
		ThoughtSignature: p.toolCall.ThoughtSignature,
	}})
	a.runToolLoop(ctx, userKey, p.connector, p.userID, p.chatID, hist, p.systemPrompt, p.tools, p.llm)
}

// newApprovalID returns a short random hex id (e.g. "a1b2c3").
func newApprovalID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is exceptional; fall back to a time-based id.
		return fmt.Sprintf("%06x", time.Now().UnixNano()&0xffffff)
	}
	return hex.EncodeToString(b)
}

// previewToolCall renders a human-readable summary of a tool call's arguments
// so the admin can see exactly what they are approving.
func previewToolCall(tc *llm.ToolCall) string {
	if len(tc.Args) == 0 {
		return "(no arguments)"
	}
	keys := make([]string, 0, len(tc.Args))
	for k := range tc.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&sb, "• %s: %s\n", k, truncate(fmt.Sprintf("%v", tc.Args[k]), 800))
	}
	return strings.TrimRight(sb.String(), "\n")
}
