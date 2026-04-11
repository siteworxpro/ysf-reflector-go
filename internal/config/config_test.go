package config

import (
	"os"
	"testing"
)

// ---------- PaddedField ----------

func TestPaddedField_ExactLength(t *testing.T) {
	b := PaddedField("ABCDEF", 6)
	if string(b) != "ABCDEF" {
		t.Fatalf("got %q, want %q", b, "ABCDEF")
	}
}

func TestPaddedField_Shorter(t *testing.T) {
	b := PaddedField("AB", 6)
	if string(b) != "AB    " {
		t.Fatalf("got %q, want %q", b, "AB    ")
	}
}

func TestPaddedField_Empty(t *testing.T) {
	b := PaddedField("", 4)
	if string(b) != "    " {
		t.Fatalf("got %q, want %q", b, "    ")
	}
}

func TestPaddedField_ZeroLen(t *testing.T) {
	b := PaddedField("X", 0)
	if len(b) != 0 {
		t.Fatalf("expected empty slice, got len %d", len(b))
	}
}

// ---------- PaddedCallsign ----------

func TestPaddedCallsign(t *testing.T) {
	cfg := &Config{Callsign: "W1AW"}
	b := cfg.PaddedCallsign()
	if len(b) != 10 {
		t.Fatalf("expected 10 bytes, got %d", len(b))
	}
	if string(b) != "W1AW      " {
		t.Fatalf("got %q", b)
	}
}

func TestPaddedCallsign_Full(t *testing.T) {
	cfg := &Config{Callsign: "1234567890"}
	b := cfg.PaddedCallsign()
	if string(b) != "1234567890" {
		t.Fatalf("got %q", b)
	}
}

// ---------- validate ----------

func newValidConfig() *Config {
	return &Config{
		Callsign: "W1AW",
		Port:     42000,
		HTTPPort: 8080,
		Timeout:  60,
	}
}

func TestValidate_HappyPath(t *testing.T) {
	cfg := newValidConfig()
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_EmptyCallsign(t *testing.T) {
	cfg := newValidConfig()
	cfg.Callsign = ""
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for empty callsign")
	}
}

func TestValidate_WhitespaceOnlyCallsign(t *testing.T) {
	cfg := newValidConfig()
	cfg.Callsign = "   "
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for whitespace-only callsign")
	}
}

func TestValidate_CallsignTooLong(t *testing.T) {
	cfg := newValidConfig()
	cfg.Callsign = "TOOLONGCALL1"
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for callsign > 10 chars")
	}
}

func TestValidate_CallsignNormalized(t *testing.T) {
	cfg := newValidConfig()
	cfg.Callsign = "  w1aw  "
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Callsign != "W1AW" {
		t.Fatalf("callsign not normalized: got %q", cfg.Callsign)
	}
}

func TestValidate_PortOutOfRange(t *testing.T) {
	cfg := newValidConfig()
	cfg.Port = 99999
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for port out of range")
	}
}

func TestValidate_PortDefault(t *testing.T) {
	cfg := newValidConfig()
	cfg.Port = 0
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 42000 {
		t.Fatalf("expected default port 42000, got %d", cfg.Port)
	}
}

func TestValidate_HTTPPortDefault(t *testing.T) {
	cfg := newValidConfig()
	cfg.HTTPPort = 0
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPPort != 8080 {
		t.Fatalf("expected default http_port 8080, got %d", cfg.HTTPPort)
	}
}

func TestValidate_HTTPPortOutOfRange(t *testing.T) {
	cfg := newValidConfig()
	cfg.HTTPPort = -1
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for http_port out of range")
	}
}

func TestValidate_TimeoutDefault(t *testing.T) {
	cfg := newValidConfig()
	cfg.Timeout = 0
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Timeout != 240 {
		t.Fatalf("expected default timeout 240, got %d", cfg.Timeout)
	}
}

func TestValidate_NameTooLong(t *testing.T) {
	cfg := newValidConfig()
	cfg.Name = "12345678901234567" // 17 chars
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for name > 16 chars")
	}
}

func TestValidate_DescriptionTooLong(t *testing.T) {
	cfg := newValidConfig()
	cfg.Description = "123456789012345" // 15 chars
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for description > 14 chars")
	}
}

// ---------- Load ----------

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/does/not/exist.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_ValidFile(t *testing.T) {
	content := `
callsign: W1AW
port: 42000
http_port: 8080
timeout: 60
id: 12345
name: TestNet
description: Test reflector
`
	f, err := os.CreateTemp(t.TempDir(), "config*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := Load(f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Callsign != "W1AW" {
		t.Errorf("callsign: got %q", cfg.Callsign)
	}
	if cfg.ID != 12345 {
		t.Errorf("id: got %d", cfg.ID)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "config*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(":::invalid yaml:::")
	f.Close()

	_, err = Load(f.Name())
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}
