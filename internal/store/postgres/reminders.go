package postgres

import (
	"context"
	"fmt"

	"github.com/sho0pi/god/internal/store"
)

func (s *Store) SaveReminder(ctx context.Context, r store.Reminder) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO reminders (connector, user_id, chat_id, schedule, instruction, enabled)
		 VALUES ($1, $2, $3, $4, $5, TRUE) RETURNING id`,
		r.Connector, r.UserID, r.ChatID, r.Schedule, r.Instruction).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("save reminder: %w", err)
	}
	return id, nil
}

func (s *Store) ListEnabledReminders(ctx context.Context) ([]store.Reminder, error) {
	return s.queryReminders(ctx,
		`SELECT id, connector, user_id, chat_id, schedule, instruction, enabled, created_at
		 FROM reminders WHERE enabled ORDER BY id`)
}

func (s *Store) ListReminders(ctx context.Context, connector, userID string) ([]store.Reminder, error) {
	return s.queryReminders(ctx,
		`SELECT id, connector, user_id, chat_id, schedule, instruction, enabled, created_at
		 FROM reminders WHERE connector=$1 AND user_id=$2 ORDER BY id`, connector, userID)
}

func (s *Store) DeleteReminder(ctx context.Context, connector, userID string, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM reminders WHERE id=$1 AND connector=$2 AND user_id=$3`, id, connector, userID)
	if err != nil {
		return false, fmt.Errorf("delete reminder: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) queryReminders(ctx context.Context, sql string, args ...any) ([]store.Reminder, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query reminders: %w", err)
	}
	defer rows.Close()

	var out []store.Reminder
	for rows.Next() {
		var r store.Reminder
		if err := rows.Scan(&r.ID, &r.Connector, &r.UserID, &r.ChatID, &r.Schedule, &r.Instruction, &r.Enabled, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan reminder: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
