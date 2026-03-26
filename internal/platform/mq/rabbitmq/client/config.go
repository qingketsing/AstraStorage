package client

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	defaultConnectionTimeout = 5 * time.Second
	defaultHeartbeat         = 10 * time.Second
	defaultConsumerPrefetch  = 32
)

type Config struct {
	Endpoints         []string
	Username          string
	Password          string
	VHost             string
	ConnectionTimeout time.Duration
	Heartbeat         time.Duration
	ConsumerPrefetch  int
	PublisherConfirm  bool
}

func (c Config) WithDefaults() Config {
	if c.ConnectionTimeout <= 0 {
		c.ConnectionTimeout = defaultConnectionTimeout
	}
	if c.Heartbeat <= 0 {
		c.Heartbeat = defaultHeartbeat
	}
	if c.ConsumerPrefetch <= 0 {
		c.ConsumerPrefetch = defaultConsumerPrefetch
	}
	if strings.TrimSpace(c.VHost) == "" {
		c.VHost = "/"
	}
	if !c.PublisherConfirm {
		c.PublisherConfirm = true
	}
	return c
}

func (c Config) Validate() error {
	if len(c.Endpoints) == 0 {
		return fmt.Errorf("rabbitmq client: at least one endpoint is required")
	}
	cfg := c.WithDefaults()
	if cfg.ConnectionTimeout <= 0 {
		return fmt.Errorf("rabbitmq client: connection timeout must be positive")
	}
	if cfg.Heartbeat <= 0 {
		return fmt.Errorf("rabbitmq client: heartbeat must be positive")
	}
	if cfg.ConsumerPrefetch <= 0 {
		return fmt.Errorf("rabbitmq client: consumer prefetch must be positive")
	}
	for _, endpoint := range cfg.Endpoints {
		if strings.TrimSpace(endpoint) == "" {
			return fmt.Errorf("rabbitmq client: endpoint cannot be empty")
		}
	}
	return nil
}

func (c Config) URLs() ([]string, error) {
	cfg := c.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	user := url.UserPassword(cfg.Username, cfg.Password).String()
	escapedVHost := url.PathEscape(cfg.VHost)
	urls := make([]string, 0, len(cfg.Endpoints))
	for _, endpoint := range cfg.Endpoints {
		urls = append(urls, fmt.Sprintf("amqp://%s@%s/%s", user, endpoint, escapedVHost))
	}
	return urls, nil
}
