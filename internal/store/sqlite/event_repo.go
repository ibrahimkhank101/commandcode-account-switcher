package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/domain"
)

type EventRepo struct {
	db *DB
}

func NewEventRepo(db *DB) *EventRepo {
	return &EventRepo{db: db}
}

func (r *EventRepo) Save(ctx context.Context, event domain.Event) error {
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	payloadJSON, _ := json.Marshal(event.Payload)

	query := "INSERT INTO events (id, type, account_id, account_alias, message, payload_json, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?)"
	_, err := r.db.ExecContext(ctx, query, event.ID, string(event.Type), event.AccountID, event.AccountAlias, event.Message, string(payloadJSON), event.Timestamp)
	return err
}

func (r *EventRepo) ListRecent(ctx context.Context, limit int) ([]domain.Event, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, "SELECT id, type, account_id, account_alias, message, payload_json, timestamp FROM events ORDER BY timestamp DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []domain.Event
	for rows.Next() {
		var e domain.Event
		var typeStr string
		var payloadJSON string

		err := rows.Scan(&e.ID, &typeStr, &e.AccountID, &e.AccountAlias, &e.Message, &payloadJSON, &e.Timestamp)
		if err != nil {
			return nil, err
		}
		e.Type = domain.EventType(typeStr)
		_ = json.Unmarshal([]byte(payloadJSON), &e.Payload)
		events = append(events, e)
	}
	return events, rows.Err()
}
