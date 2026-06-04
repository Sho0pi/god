package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"

	"github.com/sho0pi/god/internal/store"
)

// minInterval is the floor for recurring reminders, to avoid runaway scheduling.
// A var (not const) so tests can lower it.
var minInterval = 30 * time.Second

// fireTimeout bounds a single reminder run (LLM + send).
const fireTimeout = 2 * time.Minute

// RunFunc executes a fired reminder's instruction and sends the reply.
type RunFunc func(ctx context.Context, connector, userID, chatID, instruction string)

// Scheduler runs persisted reminders on time via gocron. It is created before
// the agent and wired with a RunFunc afterwards (SetRunner), breaking the
// tool→scheduler→agent dependency cycle.
type Scheduler struct {
	store store.ReminderStore
	sched gocron.Scheduler

	mu   sync.Mutex
	run  RunFunc
	jobs map[int64]uuid.UUID // reminder id → gocron job id, for cancel
}

// NewScheduler builds a Scheduler over a started-on-demand gocron instance.
func NewScheduler(s store.ReminderStore) (*Scheduler, error) {
	gs, err := gocron.NewScheduler()
	if err != nil {
		return nil, fmt.Errorf("scheduler: %w", err)
	}
	return &Scheduler{store: s, sched: gs, jobs: make(map[int64]uuid.UUID)}, nil
}

// SetRunner wires the callback used to execute a fired reminder. Call before Start.
func (s *Scheduler) SetRunner(fn RunFunc) {
	s.mu.Lock()
	s.run = fn
	s.mu.Unlock()
}

// Start loads enabled reminders, schedules them, starts gocron, and shuts it
// down when ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) error {
	reminders, err := s.store.ListEnabledReminders(ctx)
	if err != nil {
		return fmt.Errorf("load reminders: %w", err)
	}
	for _, r := range reminders {
		if err := s.schedule(r); err != nil {
			slog.Warn("scheduler: skip reminder", "id", r.ID, "err", err)
		}
	}
	s.sched.Start()
	slog.Info("scheduler: started", "reminders", len(reminders))

	go func() {
		<-ctx.Done()
		_ = s.sched.Shutdown()
	}()
	return nil
}

// Add validates, persists, and schedules a new reminder, returning its id.
func (s *Scheduler) Add(ctx context.Context, r store.Reminder) (int64, error) {
	if _, err := jobDefinition(r.Schedule); err != nil {
		return 0, err
	}
	id, err := s.store.SaveReminder(ctx, r)
	if err != nil {
		return 0, err
	}
	r.ID = id
	if err := s.schedule(r); err != nil {
		// Persisted but couldn't schedule live — roll back so we don't lie.
		_, _ = s.store.DeleteReminder(ctx, r.Connector, r.UserID, id)
		return 0, err
	}
	return id, nil
}

// Cancel unschedules and deletes a reminder owned by (connector,userID).
func (s *Scheduler) Cancel(ctx context.Context, connector, userID string, id int64) (bool, error) {
	ok, err := s.store.DeleteReminder(ctx, connector, userID, id)
	if err != nil || !ok {
		return ok, err
	}
	s.mu.Lock()
	if jobID, found := s.jobs[id]; found {
		_ = s.sched.RemoveJob(jobID)
		delete(s.jobs, id)
	}
	s.mu.Unlock()
	return true, nil
}

// List returns the reminders owned by (connector,userID).
func (s *Scheduler) List(ctx context.Context, connector, userID string) ([]store.Reminder, error) {
	return s.store.ListReminders(ctx, connector, userID)
}

// schedule registers a gocron job that fires the reminder.
func (s *Scheduler) schedule(r store.Reminder) error {
	def, err := jobDefinition(r.Schedule)
	if err != nil {
		return err
	}
	job, err := s.sched.NewJob(def, gocron.NewTask(func() {
		s.fire(r)
	}), gocron.WithSingletonMode(gocron.LimitModeReschedule))
	if err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	s.mu.Lock()
	s.jobs[r.ID] = job.ID()
	s.mu.Unlock()
	return nil
}

func (s *Scheduler) fire(r store.Reminder) {
	s.mu.Lock()
	run := s.run
	s.mu.Unlock()
	if run == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fireTimeout)
	defer cancel()
	run(ctx, r.Connector, r.UserID, r.ChatID, r.Instruction)
}

// jobDefinition turns a schedule string into a gocron job definition. A schedule
// that parses as a Go duration ("1m") is a recurring DurationJob; otherwise it is
// treated as a cron expression ("0 9 * * *").
func jobDefinition(schedule string) (gocron.JobDefinition, error) {
	if d, err := time.ParseDuration(schedule); err == nil {
		if d < minInterval {
			return nil, fmt.Errorf("interval too short (min %s)", minInterval)
		}
		return gocron.DurationJob(d), nil
	}
	// 5-field cron (no seconds). gocron validates the expression when the job runs.
	return gocron.CronJob(schedule, false), nil
}
