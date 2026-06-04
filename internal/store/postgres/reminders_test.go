package postgres_test

import (
	"testing"

	"github.com/sho0pi/god/internal/store"
	"github.com/sho0pi/god/internal/store/postgres"
)

func TestPostgres_Reminders(t *testing.T) {
	url, ctx := requireDB(t)
	s, err := postgres.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.Close()

	conn, user := "test", "rem_"+t.Name()

	id, err := s.SaveReminder(ctx, store.Reminder{
		Connector: conn, UserID: user, ChatID: "chat1", Schedule: "1m", Instruction: "say hi",
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	list, err := s.ListReminders(ctx, conn, user)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Instruction != "say hi" || list[0].Schedule != "1m" {
		t.Fatalf("unexpected list: %+v", list)
	}

	// Enabled list includes it.
	enabled, _ := s.ListEnabledReminders(ctx)
	found := false
	for _, r := range enabled {
		if r.ID == id {
			found = true
		}
	}
	if !found {
		t.Error("reminder missing from ListEnabledReminders")
	}

	// Delete is owner-scoped: a different user can't delete it.
	if ok, _ := s.DeleteReminder(ctx, conn, "someone_else", id); ok {
		t.Error("delete should be scoped to the owner")
	}
	ok, err := s.DeleteReminder(ctx, conn, user, id)
	if err != nil || !ok {
		t.Fatalf("delete: ok=%v err=%v", ok, err)
	}
	if list, _ := s.ListReminders(ctx, conn, user); len(list) != 0 {
		t.Errorf("reminder should be gone, got %d", len(list))
	}
}
