package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sho0pi/god/internal/store"
)

// memReminderStore is an in-memory ReminderStore for scheduler tests.
type memReminderStore struct {
	linkStore // no-op for the non-reminder methods
	mu        sync.Mutex
	next      int64
	items     map[int64]store.Reminder
}

func newMemReminderStore() *memReminderStore {
	return &memReminderStore{items: map[int64]store.Reminder{}}
}

func (m *memReminderStore) SaveReminder(_ context.Context, r store.Reminder) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	r.ID = m.next
	r.Enabled = true
	m.items[r.ID] = r
	return r.ID, nil
}
func (m *memReminderStore) ListEnabledReminders(context.Context) ([]store.Reminder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Reminder
	for _, r := range m.items {
		out = append(out, r)
	}
	return out, nil
}
func (m *memReminderStore) ListReminders(_ context.Context, c, u string) ([]store.Reminder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Reminder
	for _, r := range m.items {
		if r.Connector == c && r.UserID == u {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memReminderStore) DeleteReminder(_ context.Context, c, u string, id int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.items[id]; ok && r.Connector == c && r.UserID == u {
		delete(m.items, id)
		return true, nil
	}
	return false, nil
}

func TestSchedulerAddPersistsAndFires(t *testing.T) {
	st := newMemReminderStore()
	sch, err := NewScheduler(st)
	if err != nil {
		t.Fatal(err)
	}
	fired := make(chan string, 4)
	sch.SetRunner(func(_ context.Context, _, _, _, instruction string) { fired <- instruction })
	if err := sch.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	id, err := sch.Add(context.Background(), store.Reminder{
		Connector: "wa", UserID: "u1", ChatID: "c1", Schedule: "30s", Instruction: "ping",
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
	if got, _ := st.ListReminders(context.Background(), "wa", "u1"); len(got) != 1 {
		t.Fatalf("want 1 persisted reminder, got %d", len(got))
	}

	// Cancel removes it.
	ok, err := sch.Cancel(context.Background(), "wa", "u1", id)
	if err != nil || !ok {
		t.Fatalf("cancel: ok=%v err=%v", ok, err)
	}
	if got, _ := st.ListReminders(context.Background(), "wa", "u1"); len(got) != 0 {
		t.Errorf("reminder should be gone, got %d", len(got))
	}
}

func TestSchedulerFiresOnInterval(t *testing.T) {
	// Lower the floor so the test fires quickly.
	old := minInterval
	minInterval = 10 * time.Millisecond
	defer func() { minInterval = old }()

	st := newMemReminderStore()
	sch, err := NewScheduler(st)
	if err != nil {
		t.Fatal(err)
	}
	fired := make(chan struct{}, 4)
	sch.SetRunner(func(_ context.Context, _, _, _, _ string) { fired <- struct{}{} })
	_ = sch.Start(t.Context())

	if _, err := sch.Add(context.Background(), store.Reminder{
		Connector: "wa", UserID: "u", ChatID: "c", Schedule: "50ms", Instruction: "x",
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	select {
	case <-fired:
	case <-time.After(3 * time.Second):
		t.Fatal("reminder never fired")
	}
}

func TestSchedulerRejectsShortInterval(t *testing.T) {
	st := newMemReminderStore()
	sch, _ := NewScheduler(st)
	_ = sch.Start(t.Context())
	if _, err := sch.Add(context.Background(), store.Reminder{
		Connector: "wa", UserID: "u", ChatID: "c", Schedule: "1s", Instruction: "x",
	}); err == nil {
		t.Error("expected sub-floor interval to be rejected")
	}
}

func TestJobDefinition(t *testing.T) {
	if _, err := jobDefinition("1m"); err != nil {
		t.Errorf("1m should be valid: %v", err)
	}
	if _, err := jobDefinition("5s"); err == nil {
		t.Error("5s should be rejected (below floor)")
	}
	if _, err := jobDefinition("0 9 * * *"); err != nil {
		t.Errorf("cron expr should be accepted: %v", err)
	}
}
