//go:build integration

package leader

import (
	"context"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
)

func TestElector_BecomesLeader(t *testing.T) {
	t.Parallel()

	etcd := startEmbeddedEtcd(t)
	client := newTestClient(t, etcd)

	elector, err := New(client, Config{
		Prefix:     "/tests/mds/leader/single",
		InstanceID: "mds-1",
		LeaseTTL:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var startedCount atomic.Int32
	var stoppedCount atomic.Int32
	started := make(chan int64, 1)
	stopped := make(chan int64, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := elector.Run(ctx, Callbacks{
			OnStartedLeading: func(ctx context.Context, term int64) {
				startedCount.Add(1)
				started <- term
			},
			OnStoppedLeading: func(term int64) {
				stoppedCount.Add(1)
				stopped <- term
			},
		}); err != nil {
			t.Errorf("Run() error = %v", err)
		}
	}()

	term := mustReceiveTerm(t, started, "started leadership")
	if term <= 0 {
		t.Fatalf("expected positive term, got %d", term)
	}

	cancel()

	stoppedTerm := mustReceiveTerm(t, stopped, "stopped leadership")
	if stoppedTerm != term {
		t.Fatalf("expected stopped term %d, got %d", term, stoppedTerm)
	}
	if got := startedCount.Load(); got != 1 {
		t.Fatalf("expected 1 started callback, got %d", got)
	}
	if got := stoppedCount.Load(); got != 1 {
		t.Fatalf("expected 1 stopped callback, got %d", got)
	}
}

func TestElector_OnlyOneLeaderAtATime(t *testing.T) {
	t.Parallel()

	etcd := startEmbeddedEtcd(t)
	client := newTestClient(t, etcd)

	electorOne, err := New(client, Config{
		Prefix:     "/tests/mds/leader/exclusive",
		InstanceID: "mds-1",
		LeaseTTL:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New(first) error = %v", err)
	}
	electorTwo, err := New(client, Config{
		Prefix:     "/tests/mds/leader/exclusive",
		InstanceID: "mds-2",
		LeaseTTL:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New(second) error = %v", err)
	}

	var currentLeaders atomic.Int32
	var maxLeaders atomic.Int32
	started := make(chan string, 2)
	stopped := make(chan string, 2)

	start := func(name string, elector *Elector, ctx context.Context) {
		t.Helper()
		go func() {
			if err := elector.Run(ctx, Callbacks{
				OnStartedLeading: func(ctx context.Context, term int64) {
					leaders := currentLeaders.Add(1)
					updateMax(&maxLeaders, leaders)
					started <- name
				},
				OnStoppedLeading: func(term int64) {
					currentLeaders.Add(-1)
					stopped <- name
				},
			}); err != nil {
				t.Errorf("%s Run() error = %v", name, err)
			}
		}()
	}

	ctxOne, cancelOne := context.WithCancel(context.Background())
	defer cancelOne()
	ctxTwo, cancelTwo := context.WithCancel(context.Background())
	defer cancelTwo()

	start("first", electorOne, ctxOne)
	start("second", electorTwo, ctxTwo)

	firstLeader := mustReceiveName(t, started, "first leader")
	select {
	case other := <-started:
		t.Fatalf("unexpected second leader %q while %q still held leadership", other, firstLeader)
	case <-time.After(500 * time.Millisecond):
	}

	cancelOne()
	cancelTwo()

	_ = mustReceiveName(t, stopped, "stopped leader")
	if got := maxLeaders.Load(); got > 1 {
		t.Fatalf("expected max 1 leader at a time, got %d", got)
	}
}

func TestElector_FailoverTriggersNewLeader(t *testing.T) {
	t.Parallel()

	etcd := startEmbeddedEtcd(t)
	client := newTestClient(t, etcd)

	electorOne, err := New(client, Config{
		Prefix:     "/tests/mds/leader/failover",
		InstanceID: "mds-1",
		LeaseTTL:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New(first) error = %v", err)
	}
	electorTwo, err := New(client, Config{
		Prefix:     "/tests/mds/leader/failover",
		InstanceID: "mds-2",
		LeaseTTL:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New(second) error = %v", err)
	}

	started := make(chan string, 2)
	stopped := make(chan string, 2)

	run := func(name string, elector *Elector, ctx context.Context) {
		go func() {
			if err := elector.Run(ctx, Callbacks{
				OnStartedLeading: func(ctx context.Context, term int64) {
					started <- name
				},
				OnStoppedLeading: func(term int64) {
					stopped <- name
				},
			}); err != nil {
				t.Errorf("%s Run() error = %v", name, err)
			}
		}()
	}

	ctxOne, cancelOne := context.WithCancel(context.Background())
	defer cancelOne()
	ctxTwo, cancelTwo := context.WithCancel(context.Background())
	defer cancelTwo()

	run("first", electorOne, ctxOne)
	run("second", electorTwo, ctxTwo)

	firstLeader := mustReceiveName(t, started, "initial leader")
	if firstLeader != "first" && firstLeader != "second" {
		t.Fatalf("unexpected initial leader %q", firstLeader)
	}

	if firstLeader == "first" {
		cancelOne()
	} else {
		cancelTwo()
	}

	stoppedLeader := mustReceiveName(t, stopped, "stopped leader")
	if stoppedLeader != firstLeader {
		t.Fatalf("expected stopped leader %q, got %q", firstLeader, stoppedLeader)
	}

	nextLeader := mustReceiveName(t, started, "new leader after failover")
	if nextLeader == firstLeader {
		t.Fatalf("expected different leader after failover, got %q", nextLeader)
	}
}

func startEmbeddedEtcd(t *testing.T) *embed.Etcd {
	t.Helper()

	cfg := embed.NewConfig()
	cfg.Dir = t.TempDir()
	cfg.LogLevel = "error"
	cfg.Logger = "zap"

	clientURL := mustURL(t, "http://127.0.0.1:0")
	peerURL := mustURL(t, "http://127.0.0.1:0")
	cfg.ListenClientUrls = []url.URL{clientURL}
	cfg.AdvertiseClientUrls = []url.URL{clientURL}
	cfg.ListenPeerUrls = []url.URL{peerURL}
	cfg.AdvertisePeerUrls = []url.URL{peerURL}
	cfg.InitialCluster = cfg.InitialClusterFromName(cfg.Name)
	cfg.Dir = filepath.Join(cfg.Dir, "etcd")

	etcd, err := embed.StartEtcd(cfg)
	if err != nil {
		t.Fatalf("StartEtcd() error = %v", err)
	}
	t.Cleanup(func() {
		etcd.Close()
	})

	select {
	case <-etcd.Server.ReadyNotify():
	case <-time.After(10 * time.Second):
		t.Fatal("embedded etcd did not become ready")
	}

	return etcd
}

func newTestClient(t *testing.T, etcd *embed.Etcd) *clientv3.Client {
	t.Helper()

	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{etcd.Clients[0].Addr().String()},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("clientv3.New() error = %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	return client
}

func mustURL(t *testing.T, raw string) url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}
	return *parsed
}

func mustReceiveTerm(t *testing.T, ch <-chan int64, what string) int64 {
	t.Helper()
	select {
	case term := <-ch:
		return term
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return 0
	}
}

func mustReceiveName(t *testing.T, ch <-chan string, what string) string {
	t.Helper()
	select {
	case name := <-ch:
		return name
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return ""
	}
}

func updateMax(dst *atomic.Int32, candidate int32) {
	for {
		current := dst.Load()
		if candidate <= current {
			return
		}
		if dst.CompareAndSwap(current, candidate) {
			return
		}
	}
}
