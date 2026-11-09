package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/domain"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://api.commandcode.ai"
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

type WhoamiResponse struct {
	Success bool `json:"success"`
	User    struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Email    string `json:"email"`
		UserName string `json:"userName"`
	} `json:"user"`
	Org interface{} `json:"org"`
}

func (c *Client) FetchWhoami(ctx context.Context, apiKey string) (*WhoamiResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/alpha/whoami?limits=1", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "command-code")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("whoami request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("whoami returned status %d: %s", resp.StatusCode, string(body))
	}

	var res WhoamiResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("failed to parse whoami: %w", err)
	}
	return &res, nil
}

type rawCreditsResponse struct {
	Credits struct {
		BelowThreshold   bool    `json:"belowThreshold"`
		CreditThreshold  float64 `json:"creditThreshold"`
		MonthlyCredits   float64 `json:"monthlyCredits"`
		PurchasedCredits float64 `json:"purchasedCredits"`
		FreeCredits      float64 `json:"freeCredits"`
	} `json:"credits"`
	WindowLimits struct {
		Limited  bool    `json:"limited"`
		Exceeded *string `json:"exceeded"`
		FiveHour struct {
			Used     float64 `json:"used"`
			Cap      float64 `json:"cap"`
			Exceeded bool    `json:"exceeded"`
			ResetAt  int64   `json:"resetAt"`
		} `json:"fiveHour"`
		Weekly struct {
			Used     float64 `json:"used"`
			Cap      float64 `json:"cap"`
			Exceeded bool    `json:"exceeded"`
			ResetAt  int64   `json:"resetAt"`
		} `json:"weekly"`
	} `json:"windowLimits"`
}

func (c *Client) FetchCredits(ctx context.Context, apiKey string) (*domain.AccountQuota, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/alpha/billing/credits", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "command-code")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("credits request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("credits returned status %d: %s", resp.StatusCode, string(body))
	}

	var raw rawCreditsResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse credits: %w", err)
	}

	now := time.Now().UTC()
	var fiveHourReset time.Time
	if raw.WindowLimits.FiveHour.ResetAt > 0 {
		fiveHourReset = time.UnixMilli(raw.WindowLimits.FiveHour.ResetAt).UTC()
	}
	var weeklyReset time.Time
	if raw.WindowLimits.Weekly.ResetAt > 0 {
		weeklyReset = time.UnixMilli(raw.WindowLimits.Weekly.ResetAt).UTC()
	}

	var exceededStr string
	if raw.WindowLimits.Exceeded != nil {
		exceededStr = *raw.WindowLimits.Exceeded
	}

	quota := &domain.AccountQuota{
		Limited:  raw.WindowLimits.Limited,
		Exceeded: exceededStr,
		FiveHour: domain.WindowLimit{
			Used:     raw.WindowLimits.FiveHour.Used,
			Cap:      raw.WindowLimits.FiveHour.Cap,
			Exceeded: raw.WindowLimits.FiveHour.Exceeded,
			ResetAt:  fiveHourReset,
		},
		Weekly: domain.WindowLimit{
			Used:     raw.WindowLimits.Weekly.Used,
			Cap:      raw.WindowLimits.Weekly.Cap,
			Exceeded: raw.WindowLimits.Weekly.Exceeded,
			ResetAt:  weeklyReset,
		},
		Credits: domain.CreditsInfo{
			BelowThreshold:   raw.Credits.BelowThreshold,
			MonthlyCredits:   raw.Credits.MonthlyCredits,
			PurchasedCredits: raw.Credits.PurchasedCredits,
			FreeCredits:      raw.Credits.FreeCredits,
		},
		UpdatedAt: now,
	}

	return quota, nil
}

type SubscriptionResponse struct {
	Success bool `json:"success"`
	Data    struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		PlanID string `json:"planId"`
	} `json:"data"`
}

func (c *Client) FetchSubscription(ctx context.Context, apiKey string) (*SubscriptionResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/alpha/billing/subscriptions", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "command-code")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subscriptions returned status %d", resp.StatusCode)
	}

	var sub SubscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sub); err != nil {
		return nil, err
	}
	return &sub, nil
}
