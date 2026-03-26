package health

import (
	"context"
	"fmt"
)

type pinger interface {
	Ping(ctx context.Context) error
}

// Checker 对 PostgreSQL 连接池执行健康探测。
type Checker struct {
	pinger pinger
}

// NewChecker 构造一个新的健康检查器。
func NewChecker(p pinger) (*Checker, error) {
	if p == nil {
		return nil, fmt.Errorf("postgres health: pinger is nil")
	}
	return &Checker{pinger: p}, nil
}

// Ping 调用底层连接池的 Ping。
func (c *Checker) Ping(ctx context.Context) error {
	if err := c.pinger.Ping(ctx); err != nil {
		return fmt.Errorf("postgres health: ping: %w", err)
	}
	return nil
}
