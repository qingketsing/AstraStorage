package client

import (
	"testing"
	"time"
)

func TestNewConfig_RejectsEmptyEndpoints(t *testing.T) {
	_, err := NewConfig([]string{"  ", ""}, 5*time.Second)
	if err == nil {
		t.Fatalf("expected empty endpoints to be rejected")
	}
}

func TestNewConfig_TrimsEndpoints(t *testing.T) {
	cfg, err := NewConfig([]string{" http://127.0.0.1:2379 ", "http://127.0.0.1:22379"}, 5*time.Second)
	if err != nil {
		t.Fatalf("new config: %v", err)
	}

	if len(cfg.Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %#v", cfg.Endpoints)
	}
	if cfg.Endpoints[0] != "http://127.0.0.1:2379" {
		t.Fatalf("unexpected first endpoint %q", cfg.Endpoints[0])
	}
	if cfg.DialTimeout != 5*time.Second {
		t.Fatalf("unexpected dial timeout %s", cfg.DialTimeout)
	}
}
