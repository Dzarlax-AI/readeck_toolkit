package bot

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config mirrors config.example.toml. See that file for the canonical
// reference shipped with the project.
type Config struct {
	Telegram TelegramConfig `toml:"telegram"`
	Readeck  ReadeckConfig  `toml:"readeck"`
	Tenants  []Tenant       `toml:"tenants"`
}

type TelegramConfig struct {
	Token string `toml:"token"`
}

type ReadeckConfig struct {
	BaseURL string `toml:"base_url"`
}

// Tenant pairs a Telegram user with the Readeck token the bot should use
// when acting on their behalf. The Note field is only for the operator —
// the bot doesn't read it.
type Tenant struct {
	TelegramID   int64  `toml:"telegram_id"`
	ReadeckToken string `toml:"readeck_token"`
	Note         string `toml:"note,omitempty"`
}

// Load reads the TOML at path, then applies optional env overrides
// (TELEGRAM_TOKEN, READECK_BASE_URL) which are convenient for compose-style
// deploys that prefer secrets in env, not files.
func Load(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	if v := os.Getenv("TELEGRAM_TOKEN"); v != "" {
		cfg.Telegram.Token = v
	}
	if v := os.Getenv("READECK_BASE_URL"); v != "" {
		cfg.Readeck.BaseURL = v
	}
	if cfg.Telegram.Token == "" {
		return nil, fmt.Errorf("telegram.token is required")
	}
	if cfg.Readeck.BaseURL == "" {
		return nil, fmt.Errorf("readeck.base_url is required")
	}
	if len(cfg.Tenants) == 0 {
		return nil, fmt.Errorf("at least one [[tenants]] entry is required")
	}
	return &cfg, nil
}

// TokenFor returns the Readeck token for a given Telegram user id.
// The second return is false if the user is not in the tenant list.
func (c *Config) TokenFor(tgID int64) (string, bool) {
	for _, t := range c.Tenants {
		if t.TelegramID == tgID {
			return t.ReadeckToken, true
		}
	}
	return "", false
}
