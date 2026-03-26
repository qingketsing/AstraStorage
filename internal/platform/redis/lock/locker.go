package lock

import (
	"context"
	"time"

	redis "github.com/redis/go-redis/v9"
)

const compareDeleteScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`

type backend interface {
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) *redis.BoolCmd
	Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd
}

type Locker struct {
	backend    backend
	defaultTTL time.Duration
}

func NewLocker(backend backend, defaultTTL time.Duration) *Locker {
	return &Locker{
		backend:    backend,
		defaultTTL: defaultTTL,
	}
}

func (l *Locker) Acquire(ctx context.Context, key, ownerToken string, ttl time.Duration) (bool, error) {
	if l == nil || l.backend == nil {
		return false, nil
	}
	if ttl <= 0 {
		ttl = l.defaultTTL
	}
	return l.backend.SetNX(ctx, key, ownerToken, ttl).Result()
}

func (l *Locker) Release(ctx context.Context, key, ownerToken string) (bool, error) {
	if l == nil || l.backend == nil {
		return false, nil
	}
	n, err := l.backend.Eval(ctx, compareDeleteScript, []string{key}, ownerToken).Int64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}
