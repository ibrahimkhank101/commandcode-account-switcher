package domain

import (
	"time"
)

// WindowLimit tracks rate limit windows returned by Command Code's API.
type WindowLimit struct {
	Used     float64   `json:"used"`
	Cap      float64   `json:"cap"`
	Exceeded bool      `json:"exceeded"`
	ResetAt  time.Time `json:"resetAt"`
}

// CreditsInfo tracks credits metadata.
type CreditsInfo struct {
	BelowThreshold   bool    `json:"belowThreshold"`
	MonthlyCredits   float64 `json:"monthlyCredits"`
	PurchasedCredits float64 `json:"purchasedCredits"`
	FreeCredits      float64 `json:"freeCredits"`
}

// AccountQuota encapsulates complete quota telemetry for a Command Code account.
type AccountQuota struct {
	FiveHour  WindowLimit `json:"fiveHour"`
	Weekly    WindowLimit `json:"weekly"`
	Credits   CreditsInfo `json:"credits"`
	Limited   bool        `json:"limited"`
	Exceeded  string      `json:"exceeded,omitempty"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

// IsExhausted returns true if the account is currently throttled or has breached window limits.
func (q AccountQuota) IsExhausted() bool {
	if q.Exceeded != "" {
		return true
	}
	if q.FiveHour.Exceeded || q.Weekly.Exceeded || q.Credits.BelowThreshold {
		return true
	}
	if q.FiveHour.Cap > 0 && q.FiveHour.Used >= q.FiveHour.Cap {
		return true
	}
	if q.Weekly.Cap > 0 && q.Weekly.Used >= q.Weekly.Cap {
		return true
	}
	return false
}

// EffectiveResetTime determines the timestamp when this account should next become available.
func (q AccountQuota) EffectiveResetTime(now time.Time) time.Time {
	var latest time.Time
	if !q.FiveHour.ResetAt.IsZero() && q.FiveHour.ResetAt.After(latest) {
		latest = q.FiveHour.ResetAt
	}
	if !q.Weekly.ResetAt.IsZero() && q.Weekly.ResetAt.After(latest) {
		latest = q.Weekly.ResetAt
	}
	if latest.IsZero() || latest.Before(now) {
		return now.Add(15 * time.Minute)
	}
	return latest
}
