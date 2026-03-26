package client

import (
	"fmt"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type Config struct {
	Endpoints   []string
	DialTimeout time.Duration
}

func NewConfig(endpoints []string, dialTimeout time.Duration) (Config, error) {
	normalized := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint != "" {
			normalized = append(normalized, endpoint)
		}
	}
	if len(normalized) == 0 {
		return Config{}, fmt.Errorf("etcd client: at least one endpoint is required")
	}
	if dialTimeout <= 0 {
		return Config{}, fmt.Errorf("etcd client: dial timeout must be positive")
	}
	return Config{
		Endpoints:   normalized,
		DialTimeout: dialTimeout,
	}, nil
}

func New(cfg Config) (*clientv3.Client, error) {
	cfg, err := NewConfig(cfg.Endpoints, cfg.DialTimeout)
	if err != nil {
		return nil, err
	}
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("etcd client: create client: %w", err)
	}
	return client, nil
}
