package coordinator

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSupervisor_StartLeadingRunsRepairer(t *testing.T) {
	t.Parallel()

	loop := newStubLeaderLoop()
	supervisor := NewSupervisor(loop)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	supervisor.StartLeading(ctx, 11)

	runCtx := loop.waitStarted(t)
	if runCtx == nil {
		t.Fatal("expected leader-scoped context")
	}
	if got := loop.runCount.Load(); got != 1 {
		t.Fatalf("expected repair loop to start once, got %d", got)
	}
}

func TestSupervisor_StopLeadingCancelsRepairer(t *testing.T) {
	t.Parallel()

	loop := newStubLeaderLoop()
	supervisor := NewSupervisor(loop)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	supervisor.StartLeading(ctx, 22)
	runCtx := loop.waitStarted(t)

	supervisor.StopLeading(22)

	select {
	case <-runCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for leader context cancellation")
	}
	loop.waitStopped(t)
}

func TestSupervisor_RestartsForNewLeadershipTerm(t *testing.T) {
	t.Parallel()

	loop := newStubLeaderLoop()
	supervisor := NewSupervisor(loop)

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	supervisor.StartLeading(firstCtx, 101)
	firstRunCtx := loop.waitStarted(t)

	supervisor.StopLeading(101)
	loop.waitStopped(t)

	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	supervisor.StartLeading(secondCtx, 102)
	secondRunCtx := loop.waitStarted(t)

	if firstRunCtx == secondRunCtx {
		t.Fatal("expected supervisor to create a new leader-scoped context for the new term")
	}
	if got := loop.runCount.Load(); got != 2 {
		t.Fatalf("expected repair loop to start twice across two leadership terms, got %d", got)
	}
}

func TestSupervisor_StartLeadingRunsAllLoops(t *testing.T) {
	t.Parallel()

	loop1 := newStubLeaderLoop()
	loop2 := newStubLeaderLoop()
	loop3 := newStubLeaderLoop()
	supervisor := NewSupervisor(loop1, loop2, loop3)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	supervisor.StartLeading(ctx, 7)

	loop1.waitStarted(t)
	loop2.waitStarted(t)
	loop3.waitStarted(t)

	if got := loop1.runCount.Load(); got != 1 {
		t.Fatalf("expected loop1 to start once, got %d", got)
	}
	if got := loop2.runCount.Load(); got != 1 {
		t.Fatalf("expected loop2 to start once, got %d", got)
	}
	if got := loop3.runCount.Load(); got != 1 {
		t.Fatalf("expected loop3 to start once, got %d", got)
	}
}

type stubLeaderLoop struct {
	runCount atomic.Int32
	started  chan context.Context
	stopped  chan struct{}
}

func newStubLeaderLoop() *stubLeaderLoop {
	return &stubLeaderLoop{
		started: make(chan context.Context, 4),
		stopped: make(chan struct{}, 4),
	}
}

func (l *stubLeaderLoop) Run(ctx context.Context) {
	l.runCount.Add(1)
	l.started <- ctx
	<-ctx.Done()
	l.stopped <- struct{}{}
}

func (l *stubLeaderLoop) waitStarted(t *testing.T) context.Context {
	t.Helper()
	select {
	case ctx := <-l.started:
		return ctx
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for loop start")
		return nil
	}
}

func (l *stubLeaderLoop) waitStopped(t *testing.T) {
	t.Helper()
	select {
	case <-l.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for loop stop")
	}
}
