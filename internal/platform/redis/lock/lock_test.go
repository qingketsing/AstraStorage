package lock

import (
	"context"
	"errors"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

func TestLockerAcquireAndRelease(t *testing.T) {
	backend := newFakeBackend()
	locker := NewLocker(backend, 5*time.Second)
	token := "owner-a"

	acquired, err := locker.Acquire(context.Background(), "astra:lock:file:meta:file-1", token, 0)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if !acquired {
		t.Fatal("expected first acquire to succeed")
	}

	released, err := locker.Release(context.Background(), "astra:lock:file:meta:file-1", token)
	if err != nil {
		t.Fatalf("release lock: %v", err)
	}
	if !released {
		t.Fatal("expected release to succeed")
	}
}

func TestLockerAcquireRejectsSecondOwner(t *testing.T) {
	backend := newFakeBackend()
	locker := NewLocker(backend, 5*time.Second)

	acquired, err := locker.Acquire(context.Background(), "astra:lock:file:meta:file-1", "owner-a", 0)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if !acquired {
		t.Fatal("expected first owner to acquire lock")
	}

	acquired, err = locker.Acquire(context.Background(), "astra:lock:file:meta:file-1", "owner-b", 0)
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if acquired {
		t.Fatal("expected second owner to be rejected while lock is held")
	}
}

func TestLockerReleaseRejectsWrongOwner(t *testing.T) {
	backend := newFakeBackend()
	locker := NewLocker(backend, 5*time.Second)

	acquired, err := locker.Acquire(context.Background(), "astra:lock:file:meta:file-1", "owner-a", 0)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if !acquired {
		t.Fatal("expected first owner to acquire lock")
	}

	released, err := locker.Release(context.Background(), "astra:lock:file:meta:file-1", "owner-b")
	if err != nil {
		t.Fatalf("release with wrong owner: %v", err)
	}
	if released {
		t.Fatal("expected wrong owner release to fail")
	}
}

func TestLockerAcquireUsesDefaultTTL(t *testing.T) {
	backend := newFakeBackend()
	locker := NewLocker(backend, 7*time.Second)

	acquired, err := locker.Acquire(context.Background(), "astra:lock:warmup:file:file-1", "owner-a", 0)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if !acquired {
		t.Fatal("expected acquire to succeed")
	}
	if backend.lastTTL != 7*time.Second {
		t.Fatalf("expected default ttl 7s, got %s", backend.lastTTL)
	}
}

type fakeBackend struct {
	values  map[string]string
	lastTTL time.Duration
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{values: make(map[string]string)}
}

func (f *fakeBackend) SetNX(ctx context.Context, key string, value any, expiration time.Duration) *redis.BoolCmd {
	f.lastTTL = expiration
	if _, exists := f.values[key]; exists {
		return redis.NewBoolResult(false, nil)
	}
	s, ok := value.(string)
	if !ok {
		return redis.NewBoolResult(false, errors.New("fake backend expects string values"))
	}
	f.values[key] = s
	return redis.NewBoolResult(true, nil)
}

func (f *fakeBackend) Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
	if len(keys) != 1 || len(args) != 1 {
		return redis.NewCmdResult(nil, errors.New("unexpected eval arguments"))
	}
	key := keys[0]
	token, ok := args[0].(string)
	if !ok {
		return redis.NewCmdResult(nil, errors.New("unexpected owner token type"))
	}
	if current, exists := f.values[key]; exists && current == token {
		delete(f.values, key)
		return redis.NewCmdResult(int64(1), nil)
	}
	return redis.NewCmdResult(int64(0), nil)
}
