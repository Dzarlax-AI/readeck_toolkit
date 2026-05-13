package bot

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config mirrors config.example.toml. See that file for the canonical
// reference shipped with the project. Both the bot and the MCP server read
// the same file; sections they don't care about are simply ignored.
type Config struct {
	Telegram TelegramConfig `toml:"telegram"`
	Readeck  ReadeckConfig  `toml:"readeck"`
	MCP      MCPConfig      `toml:"mcp"`
	Tenants  []Tenant       `toml:"tenants"`
}

type TelegramConfig struct {
	Token string `toml:"token"`
}

type ReadeckConfig struct {
	BaseURL string `toml:"base_url"`
}

// MCPConfig configures the standalone MCP server. Tenant names the
// [[tenants]] entry (by .note) whose readeck_token the MCP uses for all
// calls — MCP is single-user by design.
type MCPConfig struct {
	Listen      string `toml:"listen"`       // default ":8080"
	Tenant      string `toml:"tenant"`       // matches [[tenants]].note
	BearerToken string `toml:"bearer_token"` // optional auth on the HTTP endpoint
}

// Tenant pairs a Telegram user with the Readeck token the bot should use
// when acting on their behalf. Note is also the lookup key for [mcp].tenant.
type Tenant struct {
	TelegramID   int64  `toml:"telegram_id"`
	ReadeckToken string `toml:"readeck_token"`
	Note         string `toml:"note,omitempty"`
}

// Load reads the TOML at path, then applies optional env overrides
// (TELEGRAM_TOKEN, READECK_BASE_URL) which are convenient for compose-style
// deploys that prefer secrets in env, not files.
//
// Validation is intentionally light: this is shared by bot and MCP, each of
// which validates further the bits it actually needs.
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
	if cfg.Readeck.BaseURL == "" {
		return nil, fmt.Errorf("readeck.base_url is required")
	}
	return &cfg, nil
}

// ValidateForBot fails if anything the bot needs is missing.
func (c *Config) ValidateForBot() error {
	if c.Telegram.Token == "" {
		return fmt.Errorf("telegram.token is required for the bot")
	}
	if len(c.Tenants) == 0 {
		return fmt.Errorf("at least one [[tenants]] entry is required for the bot")
	}
	return nil
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

// TenantByNote returns a tenant whose `note` field matches name.
func (c *Config) TenantByNote(name string) (*Tenant, bool) {
	for i := range c.Tenants {
		if c.Tenants[i].Note == name {
			return &c.Tenants[i], true
		}
	}
	return nil, false
}
