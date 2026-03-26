package coordinator

import (
	"context"
	"sync"
)

type leaderLoop interface {
	Run(ctx context.Context)
}

type Supervisor struct {
	loops []leaderLoop

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	term   int64
}

func NewSupervisor(loops ...leaderLoop) *Supervisor {
	filtered := make([]leaderLoop, 0, len(loops))
	for _, loop := range loops {
		if loop != nil {
			filtered = append(filtered, loop)
		}
	}
	return &Supervisor{loops: filtered}
}

func (s *Supervisor) StartLeading(ctx context.Context, term int64) {
	if s == nil || len(s.loops) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var waitFor chan struct{}
	s.mu.Lock()
	if s.cancel != nil {
		waitFor = s.done
		s.cancel()
		s.cancel = nil
		s.done = nil
	}
	s.term = term
	leaderCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.cancel = cancel
	s.done = done
	s.mu.Unlock()

	if waitFor != nil {
		<-waitFor
	}

	go func() {
		defer close(done)
		var wg sync.WaitGroup
		wg.Add(len(s.loops))
		for _, loop := range s.loops {
			loop := loop
			go func() {
				defer wg.Done()
				loop.Run(leaderCtx)
			}()
		}
		wg.Wait()
	}()
}

func (s *Supervisor) StopLeading(term int64) {
	if s == nil {
		return
	}

	var cancel context.CancelFunc
	var done chan struct{}

	s.mu.Lock()
	cancel = s.cancel
	done = s.done
	s.cancel = nil
	s.done = nil
	s.term = 0
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
