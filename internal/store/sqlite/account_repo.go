package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/domain"
)

type AccountRepo struct {
	db *DB
}

func NewAccountRepo(db *DB) *AccountRepo {
	return &AccountRepo{db: db}
}

func (r *AccountRepo) Create(ctx context.Context, acc *domain.Account) error {
	if acc.ID == "" {
		acc.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if acc.CreatedAt.IsZero() {
		acc.CreatedAt = now
	}
	acc.UpdatedAt = now
	if acc.LastUsedAt.IsZero() {
		acc.LastUsedAt = now
	}
	if acc.Status == "" {
		acc.Status = domain.StatusActive
	}

	quotaBytes, err := json.Marshal(acc.Quota)
	if err != nil {
		return fmt.Errorf("failed to marshal quota: %w", err)
	}

	var isActiveInt int
	if acc.IsActive {
		isActiveInt = 1
		// Deactivate others
		_, _ = r.db.ExecContext(ctx, "UPDATE accounts SET is_active = 0 WHERE is_active = 1")
	}

	query := `
	INSERT INTO accounts (
		id, alias, api_key, user_id, user_name, email, plan_id, key_name,
		is_active, status, quota_json, priority, request_count, failover_count,
		last_used_at, exhausted_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	var exhaustedAtVal interface{}
	if !acc.ExhaustedAt.IsZero() {
		exhaustedAtVal = acc.ExhaustedAt
	}

	_, err = r.db.ExecContext(ctx, query,
		acc.ID, acc.Alias, acc.APIKey, acc.UserID, acc.UserName, acc.Email, acc.PlanID, acc.KeyName,
		isActiveInt, string(acc.Status), string(quotaBytes), acc.Priority, acc.RequestCount, acc.FailoverCount,
		acc.LastUsedAt, exhaustedAtVal, acc.CreatedAt, acc.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert account: %w", err)
	}
	return nil
}

func (r *AccountRepo) scanRow(row *sql.Row) (*domain.Account, error) {
	var acc domain.Account
	var isActiveInt int
	var statusStr string
	var quotaJSON string
	var exhaustedAt sql.NullTime

	err := row.Scan(
		&acc.ID, &acc.Alias, &acc.APIKey, &acc.UserID, &acc.UserName, &acc.Email, &acc.PlanID, &acc.KeyName,
		&isActiveInt, &statusStr, &quotaJSON, &acc.Priority, &acc.RequestCount, &acc.FailoverCount,
		&acc.LastUsedAt, &exhaustedAt, &acc.CreatedAt, &acc.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrAccountNotFound
		}
		return nil, err
	}

	acc.IsActive = isActiveInt == 1
	acc.Status = domain.AccountStatus(statusStr)
	if exhaustedAt.Valid {
		acc.ExhaustedAt = exhaustedAt.Time
	}
	_ = json.Unmarshal([]byte(quotaJSON), &acc.Quota)
	return &acc, nil
}

func (r *AccountRepo) scanRows(rows *sql.Rows) ([]*domain.Account, error) {
	defer rows.Close()
	var accounts []*domain.Account

	for rows.Next() {
		var acc domain.Account
		var isActiveInt int
		var statusStr string
		var quotaJSON string
		var exhaustedAt sql.NullTime

		err := rows.Scan(
			&acc.ID, &acc.Alias, &acc.APIKey, &acc.UserID, &acc.UserName, &acc.Email, &acc.PlanID, &acc.KeyName,
			&isActiveInt, &statusStr, &quotaJSON, &acc.Priority, &acc.RequestCount, &acc.FailoverCount,
			&acc.LastUsedAt, &exhaustedAt, &acc.CreatedAt, &acc.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		acc.IsActive = isActiveInt == 1
		acc.Status = domain.AccountStatus(statusStr)
		if exhaustedAt.Valid {
			acc.ExhaustedAt = exhaustedAt.Time
		}
		_ = json.Unmarshal([]byte(quotaJSON), &acc.Quota)
		accounts = append(accounts, &acc)
	}
	return accounts, rows.Err()
}

func (r *AccountRepo) GetByID(ctx context.Context, id string) (*domain.Account, error) {
	row := r.db.QueryRowContext(ctx, "SELECT id, alias, api_key, user_id, user_name, email, plan_id, key_name, is_active, status, quota_json, priority, request_count, failover_count, last_used_at, exhausted_at, created_at, updated_at FROM accounts WHERE id = ?", id)
	return r.scanRow(row)
}

func (r *AccountRepo) GetByAlias(ctx context.Context, alias string) (*domain.Account, error) {
	row := r.db.QueryRowContext(ctx, "SELECT id, alias, api_key, user_id, user_name, email, plan_id, key_name, is_active, status, quota_json, priority, request_count, failover_count, last_used_at, exhausted_at, created_at, updated_at FROM accounts WHERE alias = ?", alias)
	return r.scanRow(row)
}

func (r *AccountRepo) GetActive(ctx context.Context) (*domain.Account, error) {
	row := r.db.QueryRowContext(ctx, "SELECT id, alias, api_key, user_id, user_name, email, plan_id, key_name, is_active, status, quota_json, priority, request_count, failover_count, last_used_at, exhausted_at, created_at, updated_at FROM accounts WHERE is_active = 1 LIMIT 1")
	acc, err := r.scanRow(row)
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			return nil, domain.ErrNoActiveAccount
		}
		return nil, err
	}
	return acc, nil
}

func (r *AccountRepo) SetActive(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, "UPDATE accounts SET is_active = 0 WHERE is_active = 1")
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, "UPDATE accounts SET is_active = 1, last_used_at = ?, updated_at = ? WHERE id = ?", now, now, id)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrAccountNotFound
	}
	return tx.Commit()
}

func (r *AccountRepo) List(ctx context.Context) ([]*domain.Account, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id, alias, api_key, user_id, user_name, email, plan_id, key_name, is_active, status, quota_json, priority, request_count, failover_count, last_used_at, exhausted_at, created_at, updated_at FROM accounts ORDER BY is_active DESC, priority ASC, created_at ASC")
	if err != nil {
		return nil, err
	}
	return r.scanRows(rows)
}

func (r *AccountRepo) Update(ctx context.Context, acc *domain.Account) error {
	acc.UpdatedAt = time.Now().UTC()
	quotaBytes, err := json.Marshal(acc.Quota)
	if err != nil {
		return fmt.Errorf("failed to marshal quota: %w", err)
	}

	var exhaustedAtVal interface{}
	if !acc.ExhaustedAt.IsZero() {
		exhaustedAtVal = acc.ExhaustedAt
	}

	var isActiveInt int
	if acc.IsActive {
		isActiveInt = 1
	}

	query := `
	UPDATE accounts SET
		alias = ?, api_key = ?, user_id = ?, user_name = ?, email = ?, plan_id = ?, key_name = ?,
		is_active = ?, status = ?, quota_json = ?, priority = ?, request_count = ?, failover_count = ?,
		last_used_at = ?, exhausted_at = ?, updated_at = ?
	WHERE id = ?
	`

	_, err = r.db.ExecContext(ctx, query,
		acc.Alias, acc.APIKey, acc.UserID, acc.UserName, acc.Email, acc.PlanID, acc.KeyName,
		isActiveInt, string(acc.Status), string(quotaBytes), acc.Priority, acc.RequestCount, acc.FailoverCount,
		acc.LastUsedAt, exhaustedAtVal, acc.UpdatedAt, acc.ID,
	)
	return err
}

func (r *AccountRepo) UpdateQuota(ctx context.Context, id string, quota domain.AccountQuota) error {
	quotaBytes, err := json.Marshal(quota)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = r.db.ExecContext(ctx, "UPDATE accounts SET quota_json = ?, updated_at = ? WHERE id = ?", string(quotaBytes), now, id)
	return err
}

func (r *AccountRepo) UpdateStatus(ctx context.Context, id string, status domain.AccountStatus, exhaustedAt time.Time) error {
	now := time.Now().UTC()
	var exhaustedVal interface{}
	if !exhaustedAt.IsZero() {
		exhaustedVal = exhaustedAt
	}
	_, err := r.db.ExecContext(ctx, "UPDATE accounts SET status = ?, exhausted_at = ?, updated_at = ? WHERE id = ?", string(status), exhaustedVal, now, id)
	return err
}

func (r *AccountRepo) RecordUsage(ctx context.Context, id string, isFailover bool) error {
	now := time.Now().UTC()
	var query string
	if isFailover {
		query = "UPDATE accounts SET request_count = request_count + 1, failover_count = failover_count + 1, last_used_at = ?, updated_at = ? WHERE id = ?"
	} else {
		query = "UPDATE accounts SET request_count = request_count + 1, last_used_at = ?, updated_at = ? WHERE id = ?"
	}
	_, err := r.db.ExecContext(ctx, query, now, now, id)
	return err
}

func (r *AccountRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM accounts WHERE id = ?", id)
	return err
}

func (r *AccountRepo) GetNextAvailable(ctx context.Context, excludeID string) (*domain.Account, error) {
	accounts, err := r.List(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	for _, acc := range accounts {
		if acc.ID == excludeID {
			continue
		}
		if acc.CanServe(now) {
			return acc, nil
		}
	}
	return nil, domain.ErrNoAvailableAccounts
}
