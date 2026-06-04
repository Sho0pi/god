package remind

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sho0pi/god/internal/store"
	"github.com/sho0pi/god/internal/tools"
)

type fakeSched struct {
	last store.Reminder
	hits int
}

func (f *fakeSched) Add(_ context.Context, r store.Reminder) (int64, error) {
	f.hits++
	f.last = r
	return 7, nil
}

func ctxWithUser(connector, userID, chatID string) context.Context {
	return context.WithValue(context.Background(), tools.UserKey{}, tools.UserInfo{
		Connector: connector, UserID: userID, ChatID: chatID,
	})
}

func TestRemindSchedules(t *testing.T) {
	fs := &fakeSched{}
	tool := New(fs)
	args, _ := json.Marshal(Args{Schedule: "1m", Instruction: "Tell the user the date."})

	res, err := tool.Execute(ctxWithUser("whatsapp", "972", "972"), args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fs.hits != 1 {
		t.Fatalf("scheduler.Add called %d times, want 1", fs.hits)
	}
	if fs.last.Connector != "whatsapp" || fs.last.ChatID != "972" || fs.last.Schedule != "1m" {
		t.Errorf("bad reminder captured: %+v", fs.last)
	}
	if res.Content == "" {
		t.Error("expected a confirmation message")
	}
}

func TestRemindRequiresChat(t *testing.T) {
	tool := New(&fakeSched{})
	args, _ := json.Marshal(Args{Schedule: "1m", Instruction: "hi"})
	// No user/chat in context.
	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Error("expected error without chat context")
	}
}

func TestRemindRequiresArgs(t *testing.T) {
	tool := New(&fakeSched{})
	args, _ := json.Marshal(Args{Schedule: "", Instruction: ""})
	if _, err := tool.Execute(ctxWithUser("wa", "u", "c"), args); err == nil {
		t.Error("expected error for empty schedule/instruction")
	}
}
