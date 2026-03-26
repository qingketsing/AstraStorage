package idempotency

import (
	"context"
	"sync"
	"time"
)

type Store interface {
	Claim(ctx context.Context, key string, ttl time.Duration) (duplicate bool, err error)
	Complete(ctx context.Context, key string, ttl time.Duration) error
	Release(ctx context.Context, key string) error
}

type MemoryStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{entries: make(map[string]time.Time)}
}

func (s *MemoryStore) Claim(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	_ = ctx
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	if expiresAt, ok := s.entries[key]; ok && expiresAt.After(now) {
		return true, nil
	}
	s.entries[key] = now.Add(ttl)
	return false, nil
}

func (s *MemoryStore) Complete(ctx context.Context, key string, ttl time.Duration) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = time.Now().UTC().Add(ttl)
	return nil
}

func (s *MemoryStore) Release(ctx context.Context, key string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
	return nil
}

func (s *MemoryStore) pruneLocked(now time.Time) {
	for key, expiresAt := range s.entries {
		if !expiresAt.After(now) {
			delete(s.entries, key)
		}
	}
}
