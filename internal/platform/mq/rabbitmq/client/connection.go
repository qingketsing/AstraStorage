package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Connection interface {
	Close() error
	IsClosed() bool
	OpenChannel() (*amqp.Channel, error)
	URL() *url.URL
}

type Dialer func(ctx context.Context, endpoint string, cfg Config) (Connection, error)

type Option func(*Manager)

type Manager struct {
	cfg            Config
	dialer         Dialer
	conn           Connection
	activeEndpoint string
}

func NewManager(cfg Config, opts ...Option) (*Manager, error) {
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	manager := &Manager{
		cfg:    cfg,
		dialer: defaultDialer,
	}
	for _, opt := range opts {
		opt(manager)
	}
	return manager, nil
}

func WithDialer(dialer Dialer) Option {
	return func(manager *Manager) {
		if dialer != nil {
			manager.dialer = dialer
		}
	}
}

func (m *Manager) Dial(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("rabbitmq client: manager is nil")
	}
	var errs []error
	for _, endpoint := range m.cfg.Endpoints {
		conn, err := m.dialer(ctx, endpoint, m.cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", endpoint, err))
			continue
		}
		if m.conn != nil && !m.conn.IsClosed() {
			_ = m.conn.Close()
		}
		m.conn = conn
		m.activeEndpoint = endpoint
		return nil
	}
	return errors.Join(errs...)
}

func (m *Manager) ActiveEndpoint() string {
	if m == nil {
		return ""
	}
	return m.activeEndpoint
}

func (m *Manager) Connection() Connection {
	if m == nil {
		return nil
	}
	return m.conn
}

func (m *Manager) Config() Config {
	if m == nil {
		return Config{}
	}
	return m.cfg
}

func (m *Manager) Close() error {
	if m == nil || m.conn == nil {
		return nil
	}
	err := m.conn.Close()
	m.conn = nil
	m.activeEndpoint = ""
	return err
}

func defaultDialer(ctx context.Context, endpoint string, cfg Config) (Connection, error) {
	uri, err := endpointURL(endpoint, cfg)
	if err != nil {
		return nil, err
	}
	conn, err := amqp.DialConfig(uri, amqp.Config{
		Heartbeat: cfg.Heartbeat,
		Dial:      amqp.DefaultDial(cfg.ConnectionTimeout),
	})
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &amqpConnection{conn: conn, uri: parsed}, nil
}

func endpointURL(endpoint string, cfg Config) (string, error) {
	for _, candidate := range cfg.Endpoints {
		if candidate == endpoint {
			user := url.UserPassword(cfg.Username, cfg.Password).String()
			return fmt.Sprintf("amqp://%s@%s/%s", user, endpoint, url.PathEscape(cfg.VHost)), nil
		}
	}
	return "", fmt.Errorf("rabbitmq client: endpoint %q is not part of config", endpoint)
}

type amqpConnection struct {
	conn *amqp.Connection
	uri  *url.URL
}

func (c *amqpConnection) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *amqpConnection) IsClosed() bool {
	if c == nil || c.conn == nil {
		return true
	}
	return c.conn.IsClosed()
}

func (c *amqpConnection) URL() *url.URL {
	if c == nil {
		return nil
	}
	return c.uri
}

func (c *amqpConnection) OpenChannel() (*amqp.Channel, error) {
	if c == nil || c.conn == nil {
		return nil, fmt.Errorf("rabbitmq client: connection is nil")
	}
	return c.conn.Channel()
}

var _ net.Error
