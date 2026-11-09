package domain

import (
	"context"
	"errors"
	"time"
)

var (
	ErrAccountNotFound    = errors.New("account not found")
	ErrAliasAlreadyExists  = errors.New("alias already exists")
	ErrNoAvailableAccounts = errors.New("no healthy accounts available in pool")
	ErrNoActiveAccount     = errors.New("no active account selected")
)

type AccountStatus string

const (
	StatusActive      AccountStatus = "active"
	StatusCoolingDown AccountStatus = "cooling_down"
	StatusExhausted   AccountStatus = "exhausted"
	StatusDisabled    AccountStatus = "disabled"
)

type Account struct {
	ID            string        `json:"id"`
	Alias         string        `json:"alias"`
	APIKey        string        `json:"apiKey,omitempty"`
	UserID        string        `json:"userId"`
	UserName      string        `json:"userName"`
	Email         string        `json:"email"`
	PlanID        string        `json:"planId"`
	KeyName       string        `json:"keyName"`
	IsActive      bool          `json:"isActive"`
	Status        AccountStatus `json:"status"`
	Quota         AccountQuota  `json:"quota"`
	Priority      int           `json:"priority"`
	RequestCount  int64         `json:"requestCount"`
	FailoverCount int64         `json:"failoverCount"`
	LastUsedAt    time.Time     `json:"lastUsedAt"`
	ExhaustedAt   time.Time     `json:"exhaustedAt,omitempty"`
	CreatedAt     time.Time     `json:"createdAt"`
	UpdatedAt     time.Time     `json:"updatedAt"`
}

func (a *Account) CanServe(now time.Time) bool {
	if a.Status == StatusDisabled {
		return false
	}
	if a.Status == StatusExhausted || a.Status == StatusCoolingDown {
		resetTime := a.Quota.EffectiveResetTime(now)
		if now.Before(resetTime) {
			return false
		}
	}
	if a.Quota.IsExhausted() {
		return false
	}
	return true
}

type AccountRepository interface {
	Create(ctx context.Context, acc *Account) error
	GetByID(ctx context.Context, id string) (*Account, error)
	GetByAlias(ctx context.Context, alias string) (*Account, error)
	GetActive(ctx context.Context) (*Account, error)
	SetActive(ctx context.Context, id string) error
	List(ctx context.Context) ([]*Account, error)
	Update(ctx context.Context, acc *Account) error
	UpdateQuota(ctx context.Context, id string, quota AccountQuota) error
	UpdateStatus(ctx context.Context, id string, status AccountStatus, exhaustedAt time.Time) error
	RecordUsage(ctx context.Context, id string, isFailover bool) error
	Delete(ctx context.Context, id string) error
	GetNextAvailable(ctx context.Context, excludeID string) (*Account, error)
}
