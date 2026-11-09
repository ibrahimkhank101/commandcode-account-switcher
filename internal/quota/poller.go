package quota

import (
	"context"
	"fmt"
	"time"

	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/api"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/domain"
)

type Poller struct {
	accountRepo domain.AccountRepository
	apiClient   *api.Client
	broadcaster domain.EventBroadcaster
	interval    time.Duration
}

func NewPoller(
	repo domain.AccountRepository,
	client *api.Client,
	broadcaster domain.EventBroadcaster,
	interval time.Duration,
) *Poller {
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	return &Poller{
		accountRepo: repo,
		apiClient:   client,
		broadcaster: broadcaster,
		interval:    interval,
	}
}

func (p *Poller) SyncAll(ctx context.Context) error {
	accounts, err := p.accountRepo.List(ctx)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, acc := range accounts {
		quota, err := p.apiClient.FetchCredits(ctx, acc.APIKey)
		if err == nil && quota != nil {
			_ = p.accountRepo.UpdateQuota(ctx, acc.ID, *quota)
			acc.Quota = *quota

			// If account was exhausted or cooling down and now healthy, reset to active
			if (acc.Status == domain.StatusExhausted || acc.Status == domain.StatusCoolingDown) && !quota.IsExhausted() {
				_ = p.accountRepo.UpdateStatus(ctx, acc.ID, domain.StatusActive, time.Time{})
				if p.broadcaster != nil {
					p.broadcaster.Publish(domain.Event{
						Type:         domain.EventQuotaRefreshed,
						AccountID:    acc.ID,
						AccountAlias: acc.Alias,
						Message:      fmt.Sprintf("Account %s recovered quota and is now active", acc.Alias),
						Timestamp:    now,
					})
				}
			}
		}
	}
	return nil
}

func (p *Poller) Start(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = p.SyncAll(ctx)
		}
	}
}
