package client

import "testing"

func TestConfig_WithDefaults(t *testing.T) {
	cfg := Config{DSN: "postgres://user:pass@localhost:5432/astra?sslmode=disable"}.WithDefaults()

	if cfg.MaxConns == 0 {
		t.Fatalf("expected max conns default to be set")
	}
	if cfg.MinConns == 0 {
		t.Fatalf("expected min conns default to be set")
	}
	if cfg.ConnectTimeout == 0 {
		t.Fatalf("expected connect timeout default to be set")
	}
}

func TestConfig_ValidateRejectsMissingDSN(t *testing.T) {
	if err := (Config{}).Validate(); err == nil {
		t.Fatalf("expected missing dsn to be rejected")
	}
}

func TestConfig_ParsePoolConfigAppliesTuning(t *testing.T) {
	cfg := Config{
		DSN:            "postgres://user:pass@localhost:5432/astra?sslmode=disable",
		MaxConns:       12,
		MinConns:       3,
		ConnectTimeout: 7,
	}

	parsed, err := cfg.ParsePoolConfig()
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	if parsed.MaxConns != 12 {
		t.Fatalf("expected max conns 12, got %d", parsed.MaxConns)
	}
	if parsed.MinConns != 3 {
		t.Fatalf("expected min conns 3, got %d", parsed.MinConns)
	}
	if parsed.ConnConfig.ConnectTimeout != 7 {
		t.Fatalf("expected connect timeout 7ns, got %s", parsed.ConnConfig.ConnectTimeout)
	}
}
