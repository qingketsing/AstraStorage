package health

import (
	"context"
	"errors"
	"testing"
)

type stubPinger struct {
	err error
}

func (s stubPinger) Ping(context.Context) error {
	return s.err
}

func TestNewCheckerRejectsNilPinger(t *testing.T) {
	if _, err := NewChecker(nil); err == nil {
		t.Fatalf("expected nil pinger to be rejected")
	}
}

func TestCheckerPingDelegates(t *testing.T) {
	checker, err := NewChecker(stubPinger{})
	if err != nil {
		t.Fatalf("new checker: %v", err)
	}
	if err := checker.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestCheckerPingWrapsErrors(t *testing.T) {
	want := errors.New("boom")
	checker, err := NewChecker(stubPinger{err: want})
	if err != nil {
		t.Fatalf("new checker: %v", err)
	}
	if err := checker.Ping(context.Background()); !errors.Is(err, want) {
		t.Fatalf("expected wrapped error %v, got %v", want, err)
	}
}
