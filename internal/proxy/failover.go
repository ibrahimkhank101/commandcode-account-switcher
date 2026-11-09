package proxy

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/domain"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/vault"
)

type FailoverEngine struct {
	mu           sync.Mutex
	accountRepo  domain.AccountRepository
	eventRepo    domain.EventRepository
	broadcaster  domain.EventBroadcaster
	vaultManager *vault.Manager
}

func NewFailoverEngine(
	repo domain.AccountRepository,
	eventRepo domain.EventRepository,
	broadcaster domain.EventBroadcaster,
	vManager *vault.Manager,
) *FailoverEngine {
	return &FailoverEngine{
		accountRepo:  repo,
		eventRepo:    eventRepo,
		broadcaster:  broadcaster,
		vaultManager: vManager,
	}
}

func IsExhaustionResponse(statusCode int, body []byte) bool {
	if statusCode == http.StatusTooManyRequests {
		return true
	}
	if statusCode == http.StatusPaymentRequired {
		return true
	}
	if (statusCode == http.StatusForbidden || statusCode == http.StatusBadRequest) && len(body) > 0 {
		b := bytes.ToLower(body)
		if bytes.Contains(b, []byte("resource_exhausted")) ||
			bytes.Contains(b, []byte("rate_limit")) ||
			bytes.Contains(b, []byte("belowthreshold")) ||
			bytes.Contains(b, []byte("window limits exceeded")) ||
			bytes.Contains(b, []byte("insufficient_quota")) ||
			bytes.Contains(b, []byte("credit")) && bytes.Contains(b, []byte("exhausted")) {
			return true
		}
	}
	return false
}

func (e *FailoverEngine) HandleExhaustion(ctx context.Context, exhaustedAcc *domain.Account, reason string) (*domain.Account, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now().UTC()
	resetAt := exhaustedAcc.Quota.EffectiveResetTime(now)
	if resetAt.Before(now) {
		resetAt = now.Add(15 * time.Minute)
	}

	_ = e.accountRepo.UpdateStatus(ctx, exhaustedAcc.ID, domain.StatusExhausted, resetAt)

	// Emit exhaustion event
	exhaustEvent := domain.Event{
		Type:         domain.EventQuotaExhausted,
		AccountID:    exhaustedAcc.ID,
		AccountAlias: exhaustedAcc.Alias,
		Message:      fmt.Sprintf("Account %s exhausted quota: %s. Cooling down until %s", exhaustedAcc.Alias, reason, resetAt.Format(time.RFC3339)),
		Payload: map[string]interface{}{
			"reason":  reason,
			"resetAt": resetAt,
		},
		Timestamp: now,
	}
	if e.eventRepo != nil {
		_ = e.eventRepo.Save(ctx, exhaustEvent)
	}
	if e.broadcaster != nil {
		e.broadcaster.Publish(exhaustEvent)
	}

	// Select next available
	nextAcc, err := e.accountRepo.GetNextAvailable(ctx, exhaustedAcc.ID)
	if err != nil {
		return nil, fmt.Errorf("all accounts in pool are exhausted: %w", err)
	}

	if err := e.accountRepo.SetActive(ctx, nextAcc.ID); err != nil {
		return nil, fmt.Errorf("failed to activate next account: %w", err)
	}

	// Sync ~/.commandcode/auth.json atomically
	if e.vaultManager != nil {
		auth := &vault.AuthFile{
			APIKey:          nextAcc.APIKey,
			UserID:          nextAcc.UserID,
			UserName:        nextAcc.UserName,
			KeyName:         nextAcc.KeyName,
			AuthenticatedAt: nextAcc.LastUsedAt.Format(time.RFC3339),
		}
		_ = e.vaultManager.AtomicWriteAuth(auth)
	}

	// Emit switch event
	switchEvent := domain.Event{
		Type:         domain.EventFailoverSuccess,
		AccountID:    nextAcc.ID,
		AccountAlias: nextAcc.Alias,
		Message:      fmt.Sprintf("Failover rotation: successfully switched from %s to %s", exhaustedAcc.Alias, nextAcc.Alias),
		Payload: map[string]interface{}{
			"previousAccount": exhaustedAcc.Alias,
			"activeAccount":   nextAcc.Alias,
		},
		Timestamp: now,
	}
	if e.eventRepo != nil {
		_ = e.eventRepo.Save(ctx, switchEvent)
	}
	if e.broadcaster != nil {
		e.broadcaster.Publish(switchEvent)
	}

	return nextAcc, nil
}
