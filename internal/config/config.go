package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// BridgeConfig defines a connection to a remote YSF reflector.
type BridgeConfig struct {
	Name           string `yaml:"name"`         // Human-readable label
	Host           string `yaml:"host"`         // Remote reflector hostname or IP
	Port           int    `yaml:"port"`         // Remote reflector UDP port, default 42000
	Callsign       string `yaml:"callsign"`     // Override callsign sent to remote (max 10 chars)
	ConnectCron    string `yaml:"connect"`      // 5-field cron expression: when to connect
	DisconnectCron string `yaml:"disconnect"`   // 5-field cron expression: when to disconnect
	AlwaysOn       bool   `yaml:"always_on"`    // Connect on startup and stay connected
	Enabled        bool   `yaml:"enabled"`      // Must be true to activate
}

// Config holds the reflector configuration.
type Config struct {
	Callsign       string         `yaml:"callsign"`        // 10-char max, space-padded
	Port           int            `yaml:"port"`            // UDP port, default 42000
	HTTPPort       int            `yaml:"http_port"`       // HTTP dashboard port, default 8080
	Timeout        int            `yaml:"timeout"`         // Client idle timeout in seconds, default 240
	Debug          bool           `yaml:"debug"`           // Log every packet
	Parrot         bool           `yaml:"parrot"`          // Buffer transmissions and replay after TX ends
	ID             uint32         `yaml:"id"`              // Numeric ID reported in YSFS status packets
	Name           string         `yaml:"name"`            // Reflector name, max 16 chars
	Description    string         `yaml:"description"`     // Short description, max 14 chars
	AllowedOrigins []string       `yaml:"allowed_origins"` // WebSocket origin allowlist (empty = same-origin only)
	Bridges        []BridgeConfig `yaml:"bridges"`         // Outbound bridges to remote reflectors
}

// Load reads and validates a YAML config file at the given path.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer func() { _ = f.Close() }()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	return &cfg, cfg.validate()
}

func (c *Config) validate() error {
	c.Callsign = strings.ToUpper(strings.TrimSpace(c.Callsign))
	if c.Callsign == "" {
		return fmt.Errorf("callsign must not be empty")
	}
	if len(c.Callsign) > 10 {
		return fmt.Errorf("callsign %q exceeds 10 characters", c.Callsign)
	}
	if c.Port == 0 {
		c.Port = 42000
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port %d out of range", c.Port)
	}
	if c.HTTPPort == 0 {
		c.HTTPPort = 8080
	}
	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		return fmt.Errorf("http_port %d out of range", c.HTTPPort)
	}
	if c.Timeout == 0 {
		c.Timeout = 240
	}
	if len(c.Name) > 16 {
		return fmt.Errorf("name %q exceeds 16 characters", c.Name)
	}
	if len(c.Description) > 14 {
		return fmt.Errorf("description %q exceeds 14 characters", c.Description)
	}
	for i, b := range c.Bridges {
		if b.Host == "" {
			return fmt.Errorf("bridges[%d]: host must not be empty", i)
		}
		if c.Bridges[i].Port == 0 {
			c.Bridges[i].Port = 42000
		}
		if c.Bridges[i].Port < 1 || c.Bridges[i].Port > 65535 {
			return fmt.Errorf("bridges[%d]: port %d out of range", i, b.Port)
		}
		if b.Callsign != "" {
			c.Bridges[i].Callsign = strings.ToUpper(strings.TrimSpace(b.Callsign))
			if len(c.Bridges[i].Callsign) > 10 {
				return fmt.Errorf("bridges[%d]: callsign %q exceeds 10 characters", i, b.Callsign)
			}
		}
	}
	return nil
}

// PaddedField returns s left-justified and space-padded to exactly n bytes.
func PaddedField(s string, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	copy(b, s)
	return b
}

// PaddedCallsign returns the callsign left-justified and space-padded to exactly 10 bytes.
func (c *Config) PaddedCallsign() []byte {
	return PaddedField(c.Callsign, 10)
}
