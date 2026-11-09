package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/api"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/config"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/domain"
)

type AuthFile struct {
	APIKey          string `json:"apiKey"`
	UserID          string `json:"userId"`
	UserName        string `json:"userName"`
	KeyName         string `json:"keyName"`
	AuthenticatedAt string `json:"authenticatedAt"`
}

type Manager struct {
	cfg       *config.Config
	accountRepo domain.AccountRepository
	apiClient   *api.Client
}

func NewManager(cfg *config.Config, repo domain.AccountRepository, client *api.Client) *Manager {
	return &Manager{
		cfg:         cfg,
		accountRepo: repo,
		apiClient:   client,
	}
}

func (m *Manager) ReadCurrentAuth() (*AuthFile, error) {
	data, err := os.ReadFile(m.cfg.CommandCodeAuthPath)
	if err != nil {
		return nil, err
	}
	var auth AuthFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("invalid auth.json format: %w", err)
	}
	return &auth, nil
}

func (m *Manager) AtomicWriteAuth(auth *AuthFile) error {
	dir := filepath.Dir(m.cfg.CommandCodeAuthPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", m.cfg.CommandCodeAuthPath, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp auth file: %w", err)
	}

	// On Windows, os.Rename fails if target exists in older runtimes, so remove old target if present
	_ = os.Remove(m.cfg.CommandCodeAuthPath)
	if err := os.Rename(tmpPath, m.cfg.CommandCodeAuthPath); err != nil {
		// Fallback direct write
		_ = os.WriteFile(m.cfg.CommandCodeAuthPath, data, 0600)
		_ = os.Remove(tmpPath)
	}
	return nil
}

func (m *Manager) SaveAccountToVault(alias string, auth *AuthFile) error {
	accDir := filepath.Join(m.cfg.VaultDir, alias)
	if err := os.MkdirAll(accDir, 0755); err != nil {
		return err
	}
	vaultPath := filepath.Join(accDir, "auth.json")
	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(vaultPath, data, 0600)
}

func (m *Manager) AutoImportActive(ctx context.Context) (*domain.Account, error) {
	auth, err := m.ReadCurrentAuth()
	if err != nil {
		return nil, err
	}
	if auth.APIKey == "" {
		return nil, fmt.Errorf("no apiKey in auth.json")
	}

	// Check if already in DB
	accounts, err := m.accountRepo.List(ctx)
	if err == nil {
		for _, acc := range accounts {
			if acc.APIKey == auth.APIKey {
				_ = m.accountRepo.SetActive(ctx, acc.ID)
				return acc, nil
			}
		}
	}

	// Verify with API
	whoami, err := m.apiClient.FetchWhoami(ctx, auth.APIKey)
	if err != nil {
		return nil, fmt.Errorf("failed to verify apiKey with command code API: %w", err)
	}

	alias := auth.UserName
	if alias == "" {
		alias = whoami.User.UserName
	}
	if alias == "" {
		alias = "primary"
	}

	quota, _ := m.apiClient.FetchCredits(ctx, auth.APIKey)
	sub, _ := m.apiClient.FetchSubscription(ctx, auth.APIKey)

	planID := "free"
	if sub != nil && sub.Data.PlanID != "" {
		planID = sub.Data.PlanID
	}

	var q domain.AccountQuota
	if quota != nil {
		q = *quota
	}

	acc := &domain.Account{
		Alias:    alias,
		APIKey:   auth.APIKey,
		UserID:   whoami.User.ID,
		UserName: whoami.User.UserName,
		Email:    whoami.User.Email,
		PlanID:   planID,
		KeyName:  auth.KeyName,
		IsActive: true,
		Status:   domain.StatusActive,
		Quota:    q,
		Priority: 0,
	}

	if err := m.accountRepo.Create(ctx, acc); err != nil {
		return nil, err
	}

	_ = m.SaveAccountToVault(alias, auth)
	return acc, nil
}
