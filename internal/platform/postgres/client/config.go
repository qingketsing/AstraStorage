package client

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultMaxConns          int32 = 8
	defaultMinConns          int32 = 1
	defaultConnectTimeout          = 5 * time.Second
	defaultHealthCheckPeriod       = 30 * time.Second
	defaultMaxConnIdleTime         = 5 * time.Minute
	defaultMaxConnLifetime         = 30 * time.Minute
)

// Config 描述 PostgreSQL 连接池初始化所需配置。
type Config struct {
	DSN               string
	MaxConns          int32
	MinConns          int32
	ConnectTimeout    time.Duration
	HealthCheckPeriod time.Duration
	MaxConnIdleTime   time.Duration
	MaxConnLifetime   time.Duration
}

// WithDefaults 返回补齐默认值后的配置副本。
func (c Config) WithDefaults() Config {
	if c.MaxConns <= 0 {
		c.MaxConns = defaultMaxConns
	}
	if c.MinConns < 0 {
		c.MinConns = 0
	}
	if c.MinConns == 0 {
		c.MinConns = defaultMinConns
	}
	if c.ConnectTimeout <= 0 {
		c.ConnectTimeout = defaultConnectTimeout
	}
	if c.HealthCheckPeriod <= 0 {
		c.HealthCheckPeriod = defaultHealthCheckPeriod
	}
	if c.MaxConnIdleTime <= 0 {
		c.MaxConnIdleTime = defaultMaxConnIdleTime
	}
	if c.MaxConnLifetime <= 0 {
		c.MaxConnLifetime = defaultMaxConnLifetime
	}
	return c
}

// Validate 校验配置的基本合法性。
func (c Config) Validate() error {
	if c.DSN == "" {
		return fmt.Errorf("postgres client: dsn is required")
	}
	if c.MaxConns < 0 {
		return fmt.Errorf("postgres client: max conns cannot be negative")
	}
	if c.MinConns < 0 {
		return fmt.Errorf("postgres client: min conns cannot be negative")
	}
	if c.MaxConns > 0 && c.MinConns > c.MaxConns {
		return fmt.Errorf("postgres client: min conns %d cannot exceed max conns %d", c.MinConns, c.MaxConns)
	}
	return nil
}

// ParsePoolConfig 把业务配置转换成 pgx 连接池配置。
func (c Config) ParsePoolConfig() (*pgxpool.Config, error) {
	cfg := c.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	parsed, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres client: parse dsn: %w", err)
	}

	parsed.MaxConns = cfg.MaxConns
	parsed.MinConns = cfg.MinConns
	parsed.HealthCheckPeriod = cfg.HealthCheckPeriod
	parsed.MaxConnIdleTime = cfg.MaxConnIdleTime
	parsed.MaxConnLifetime = cfg.MaxConnLifetime
	parsed.ConnConfig.ConnectTimeout = cfg.ConnectTimeout

	return parsed, nil
}

// NewPool 创建 PostgreSQL 连接池并在返回前完成一次 Ping。
func NewPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	parsed, err := cfg.ParsePoolConfig()
	if err != nil {
		return nil, err
	}

	connectCtx := ctx
	var cancel context.CancelFunc
	if cfgWithDefaults := cfg.WithDefaults(); cfgWithDefaults.ConnectTimeout > 0 {
		connectCtx, cancel = context.WithTimeout(ctx, cfgWithDefaults.ConnectTimeout)
		defer cancel()
	}

	pool, err := pgxpool.NewWithConfig(connectCtx, parsed)
	if err != nil {
		return nil, fmt.Errorf("postgres client: create pool: %w", err)
	}
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres client: ping: %w", err)
	}

	return pool, nil
}
