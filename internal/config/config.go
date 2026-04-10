package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds the reflector configuration.
type Config struct {
	Callsign    string `yaml:"callsign"`    // 10-char max, space-padded
	Port        int    `yaml:"port"`        // UDP port, default 42000
	HTTPPort    int    `yaml:"http_port"`   // HTTP dashboard port, default 8080
	Timeout     int    `yaml:"timeout"`     // Client idle timeout in seconds, default 240
	Debug       bool   `yaml:"debug"`       // Log every packet
	ID          uint32 `yaml:"id"`          // Numeric ID reported in YSFS status packets
	Name        string `yaml:"name"`        // Reflector name, max 16 chars
	Description string `yaml:"description"` // Short description, max 14 chars
}

// Load reads and validates a YAML config file at the given path.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

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
