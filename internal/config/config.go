package config

import (
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	Port                int           `json:"port"`
	TargetURL           string        `json:"targetUrl"`
	DBPath              string        `json:"dbPath"`
	VaultDir            string        `json:"vaultDir"`
	CommandCodeAuthPath string        `json:"commandCodeAuthPath"`
	MaxRetries          int           `json:"maxRetries"`
	MaxBodyBytes        int64         `json:"maxBodyBytes"`
	PollInterval        time.Duration `json:"pollInterval"`
	AutoImportOnStart   bool          `json:"autoImportOnStart"`
}

func DefaultConfig() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	switcherDir := filepath.Join(homeDir, ".commandcode-switcher")
	_ = os.MkdirAll(switcherDir, 0755)

	vaultDir := filepath.Join(homeDir, ".commandcode", "accounts")
	_ = os.MkdirAll(vaultDir, 0755)

	authPath := filepath.Join(homeDir, ".commandcode", "auth.json")
	dbPath := filepath.Join(switcherDir, "switcher.db")

	return &Config{
		Port:                8766,
		TargetURL:           "https://api.commandcode.ai",
		DBPath:              dbPath,
		VaultDir:            vaultDir,
		CommandCodeAuthPath: authPath,
		MaxRetries:          3,
		MaxBodyBytes:        32 * 1024 * 1024, // 32MB buffer
		PollInterval:        2 * time.Minute,
		AutoImportOnStart:   true,
	}, nil
}
