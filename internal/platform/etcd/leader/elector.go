package leader

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

const defaultLeaseTTL = 10 * time.Second

type Config struct {
	Prefix     string
	InstanceID string
	LeaseTTL   time.Duration
}

type Callbacks struct {
	OnStartedLeading func(ctx context.Context, term int64)
	OnStoppedLeading func(term int64)
}

type Elector struct {
	client *clientv3.Client
	cfg    Config
}

func New(client *clientv3.Client, cfg Config) (*Elector, error) {
	if client == nil {
		return nil, fmt.Errorf("etcd leader: client is nil")
	}
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Elector{client: client, cfg: cfg}, nil
}

func (e *Elector) Run(ctx context.Context, callbacks Callbacks) error {
	if e == nil {
		return fmt.Errorf("etcd leader: elector is nil")
	}
	if ctx == nil {
		return fmt.Errorf("etcd leader: context is nil")
	}

	for {
		if ctx.Err() != nil {
			return nil
		}

		session, err := concurrency.NewSession(
			e.client,
			concurrency.WithContext(ctx),
			concurrency.WithTTL(ttlSeconds(e.cfg.LeaseTTL)),
		)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("etcd leader: create session: %w", err)
		}

		runErr := e.runElection(ctx, session, callbacks)
		closeErr := session.Close()
		if runErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return runErr
		}
		if closeErr != nil && !errors.Is(closeErr, context.Canceled) && ctx.Err() == nil {
			return fmt.Errorf("etcd leader: close session: %w", closeErr)
		}
	}
}

func (e *Elector) runElection(ctx context.Context, session *concurrency.Session, callbacks Callbacks) error {
	election := concurrency.NewElection(session, e.cfg.Prefix)
	sessionCtx, stopSessionCtx := sessionContext(ctx, session)
	defer stopSessionCtx()

	if err := election.Campaign(sessionCtx, e.cfg.InstanceID); err != nil {
		if ctx.Err() != nil || sessionExpired(session) {
			return nil
		}
		return fmt.Errorf("etcd leader: campaign: %w", err)
	}

	term, err := currentTerm(sessionCtx, election)
	if err != nil {
		if ctx.Err() != nil || sessionExpired(session) {
			return nil
		}
		return fmt.Errorf("etcd leader: resolve term: %w", err)
	}

	leaderCtx, cancel := context.WithCancel(ctx)
	if callbacks.OnStartedLeading != nil {
		callbacks.OnStartedLeading(leaderCtx, term)
	}

	select {
	case <-ctx.Done():
	case <-session.Done():
	}

	cancel()
	if callbacks.OnStoppedLeading != nil {
		callbacks.OnStoppedLeading(term)
	}

	resignCtx, resignCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer resignCancel()
	if err := election.Resign(resignCtx); err != nil && !errors.Is(err, concurrency.ErrElectionNotLeader) {
		if ctx.Err() != nil || sessionExpired(session) {
			return nil
		}
		return fmt.Errorf("etcd leader: resign: %w", err)
	}
	return nil
}

func currentTerm(ctx context.Context, election *concurrency.Election) (int64, error) {
	termCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := election.Leader(termCtx)
	if err != nil {
		return 0, err
	}
	if len(resp.Kvs) == 0 {
		return 0, fmt.Errorf("etcd leader: missing leader key")
	}
	return resp.Kvs[0].ModRevision, nil
}

func (c Config) withDefaults() Config {
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = defaultLeaseTTL
	}
	return c
}

func (c Config) validate() error {
	if strings.TrimSpace(c.Prefix) == "" {
		return fmt.Errorf("etcd leader: prefix is required")
	}
	if strings.TrimSpace(c.InstanceID) == "" {
		return fmt.Errorf("etcd leader: instance id is required")
	}
	if c.LeaseTTL <= 0 {
		return fmt.Errorf("etcd leader: lease ttl must be positive")
	}
	return nil
}

func ttlSeconds(d time.Duration) int {
	seconds := int(math.Ceil(d.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}

func sessionContext(parent context.Context, session *concurrency.Session) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		select {
		case <-session.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func sessionExpired(session *concurrency.Session) bool {
	select {
	case <-session.Done():
		return true
	default:
		return false
	}
}
