package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	mdsconfig "AstraStorage/internal/mds/config"
	"AstraStorage/internal/mds/metadata"
	mdsmq "AstraStorage/internal/mds/mq"
	mdsrpc "AstraStorage/internal/mds/rpc"
	"AstraStorage/internal/mds/store"

	leaderpkg "AstraStorage/internal/platform/etcd/leader"
	"AstraStorage/internal/platform/mq/contracts"
	rabbitmqclient "AstraStorage/internal/platform/mq/rabbitmq/client"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestNewApplication_BootstrapsDependencyChain(t *testing.T) {
	app, err := newApplication(context.Background())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	defer app.Close()
	if app.repo == nil || app.service == nil || app.handler == nil || app.router == nil || app.httpServer == nil {
		t.Fatalf("expected repo/service/handler/router/httpServer to be initialized")
	}

	root, err := app.repo.GetInode(context.Background(), store.InodeSelector{ID: metadata.InodeID(metadata.RootInodeID)})
	if err != nil {
		t.Fatalf("get root inode: %v", err)
	}
	if root.Path != "/" {
		t.Fatalf("expected root path /, got %q", root.Path)
	}
	if app.httpAddr == "" {
		t.Fatalf("expected http addr to be initialized")
	}
}

func TestNewApplication_RouterCanServeCreateFileFlow(t *testing.T) {
	app, err := newApplication(context.Background())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	defer app.Close()

	now := time.Now()
	result, err := app.router.Dispatch(context.Background(), mdsrpc.MethodCreateFile, mdsrpc.CreateFileRequest{
		InodeID:   "cmd-file-inode",
		FileID:    "cmd-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "cmd.txt",
		Size:      32,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("dispatch create file: %v", err)
	}
	resp, ok := result.(*mdsrpc.CreateFileResponse)
	if !ok {
		t.Fatalf("expected CreateFileResponse, got %T", result)
	}
	if resp.File == nil || resp.File.ID != "cmd-file" {
		t.Fatalf("expected created file cmd-file, got %#v", resp.File)
	}
}

func TestNewApplicationWithConfig_PostgresRequiresDSN(t *testing.T) {
	_, err := newApplicationWithConfig(context.Background(), mdsconfig.Config{Backend: mdsconfig.BackendPostgres})
	if err == nil {
		t.Fatalf("expected postgres config without dsn to fail")
	}
	if err.Error() == "" {
		t.Fatalf("expected bootstrap error, got %v", err)
	}
}

func TestNewApplicationWithConfig_EnablesGRPCServer(t *testing.T) {
	app, err := newApplicationWithConfig(context.Background(), mdsconfig.Config{
		Backend: mdsconfig.BackendMemory,
		HTTP:    mdsconfig.HTTPConfig{Addr: ":8080"}.WithDefaults(),
		GRPC:    mdsconfig.GRPCConfig{Addr: ":9090"},
	})
	if err != nil {
		t.Fatalf("new application with grpc config: %v", err)
	}
	defer app.Close()
	if app.grpcServer == nil {
		t.Fatalf("expected grpc server to be initialized")
	}
	if app.grpcAddr != ":9090" {
		t.Fatalf("expected grpc addr :9090, got %q", app.grpcAddr)
	}
}

func TestNewApplication_HTTPServerServesHealthz(t *testing.T) {
	app, err := newApplication(context.Background())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	defer app.Close()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	app.httpServer.Handler.ServeHTTP(recorder, req)
	resp := recorder.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from healthz, got %d", resp.StatusCode)
	}

	var payload struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode healthz response: %v", err)
	}
	if payload.Status != "ok" {
		t.Fatalf("expected healthz status ok, got %#v", payload)
	}
}

func TestNewApplication_HTTPServerServesMetrics(t *testing.T) {
	app, err := newApplication(context.Background())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	defer app.Close()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	app.httpServer.Handler.ServeHTTP(recorder, req)
	resp := recorder.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from metrics, got %d", resp.StatusCode)
	}
	body := new(bytes.Buffer)
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read metrics body: %v", err)
	}
	if !strings.Contains(body.String(), "go_goroutines") {
		t.Fatalf("expected built-in prometheus metric line in body, got %q", body.String())
	}
}

func TestNewApplicationWithConfig_LeaderElectionEnabledBuildsElector(t *testing.T) {
	previousClientFactory := etcdClientFactory
	previousElectorFactory := leaderElectorFactory
	t.Cleanup(func() {
		etcdClientFactory = previousClientFactory
		leaderElectorFactory = previousElectorFactory
	})

	clientCalled := false
	electorCalled := false
	etcdClientFactory = func(cfg mdsconfig.LeaderElectionConfig) (*clientv3.Client, error) {
		clientCalled = true
		if len(cfg.EtcdEndpoints) != 1 || cfg.EtcdEndpoints[0] != "http://127.0.0.1:2379" {
			t.Fatalf("unexpected etcd endpoints: %#v", cfg.EtcdEndpoints)
		}
		return nil, nil
	}
	leaderElectorFactory = func(client *clientv3.Client, cfg mdsconfig.LeaderElectionConfig) (leaderElector, error) {
		electorCalled = true
		if got := instanceIDForLeaderElection(cfg.InstanceID); got == "" {
			t.Fatal("expected generated instance id")
		}
		return stubLeaderElector{}, nil
	}

	app, err := newApplicationWithConfig(context.Background(), mdsconfig.Config{
		Backend: mdsconfig.BackendMemory,
		HTTP:    mdsconfig.HTTPConfig{Addr: ":8080"}.WithDefaults(),
		LeaderElection: mdsconfig.LeaderElectionConfig{
			Enabled:       true,
			EtcdEndpoints: []string{"http://127.0.0.1:2379"},
			DialTimeout:   5 * time.Second,
			Prefix:        "/astra/mds/leader",
			LeaseTTL:      10 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("new application with leader election: %v", err)
	}
	if !clientCalled {
		t.Fatal("expected etcd client factory to be called")
	}
	if !electorCalled {
		t.Fatal("expected leader elector factory to be called")
	}
	if app.supervisor == nil {
		t.Fatal("expected supervisor to be initialized")
	}
	if app.failoverPlanner == nil || app.cleanupController == nil || app.rebalancePlanner == nil {
		t.Fatal("expected scheduling loops to be initialized")
	}
	if app.elector == nil {
		t.Fatal("expected elector to be initialized")
	}
}

func TestNewApplicationWithConfig_RedisEnabledBuildsBundle(t *testing.T) {
	app, err := newApplicationWithConfig(context.Background(), mdsconfig.Config{
		Backend: mdsconfig.BackendMemory,
		HTTP:    mdsconfig.HTTPConfig{Addr: ":8080"}.WithDefaults(),
		Redis: mdsconfig.RedisConfig{
			Enabled:           true,
			SentinelEndpoints: []string{"127.0.0.1:26379", "127.0.0.1:26380", "127.0.0.1:26381"},
			Cache: mdsconfig.RedisReplicationGroupConfig{
				MasterSetName: "astra-cache",
			},
			Coord: mdsconfig.RedisReplicationGroupConfig{
				MasterSetName: "astra-coord",
			},
		},
	})
	if err != nil {
		t.Fatalf("new application with redis: %v", err)
	}
	defer app.Close()

	if app.redis == nil {
		t.Fatal("expected redis bundle to be initialized")
	}
	if app.redis.Cache() == nil || app.redis.Coord() == nil {
		t.Fatal("expected cache and coord redis groups to be initialized")
	}
	if app.warmup == nil {
		t.Fatal("expected redis warmup runner to be initialized")
	}
}

func TestNewApplicationWithConfig_RabbitMQEnabledBuildsTaskProducer(t *testing.T) {
	previousManagerFactory := rabbitMQManagerFactory
	previousProducerFactory := rabbitMQTaskProducerFactory
	t.Cleanup(func() {
		rabbitMQManagerFactory = previousManagerFactory
		rabbitMQTaskProducerFactory = previousProducerFactory
	})

	managerCalled := false
	producerCalled := false
	manager := &rabbitmqclient.Manager{}
	rabbitMQManagerFactory = func(cfg mdsconfig.RabbitMQConfig) (*rabbitmqclient.Manager, error) {
		managerCalled = true
		if len(cfg.Endpoints) != 1 || cfg.Endpoints[0] != "127.0.0.1:5672" {
			t.Fatalf("unexpected rabbitmq endpoints: %#v", cfg.Endpoints)
		}
		return manager, nil
	}
	rabbitMQTaskProducerFactory = func(got *rabbitmqclient.Manager) (mdsmq.TaskProducer, error) {
		producerCalled = true
		if got != manager {
			t.Fatalf("unexpected manager instance: %#v", got)
		}
		return stubTaskProducer{}, nil
	}

	app, err := newApplicationWithConfig(context.Background(), mdsconfig.Config{
		Backend: mdsconfig.BackendMemory,
		HTTP:    mdsconfig.HTTPConfig{Addr: ":8080"}.WithDefaults(),
		RabbitMQ: mdsconfig.RabbitMQConfig{
			Enabled: true,
			Config: rabbitmqclient.Config{
				Endpoints: []string{"127.0.0.1:5672"},
				Username:  "astra",
				Password:  "astra-dev",
				VHost:     "/astra",
			},
		},
	})
	if err != nil {
		t.Fatalf("new application with rabbitmq: %v", err)
	}
	defer app.Close()

	if !managerCalled {
		t.Fatal("expected rabbitmq manager factory to be called")
	}
	if !producerCalled {
		t.Fatal("expected rabbitmq task producer factory to be called")
	}
	if app.rabbitmq != manager {
		t.Fatal("expected rabbitmq manager to be stored on application")
	}
	if app.taskProducer == nil {
		t.Fatal("expected task producer to be initialized")
	}
}

func TestApplication_StartCoordinatorSingleNodeStartsSupervisor(t *testing.T) {
	supervisor := &stubCoordinatorSupervisor{}
	app := &application{supervisor: supervisor}
	errCh := make(chan error, 1)

	if running := app.startCoordinator(context.Background(), errCh); running != 0 {
		t.Fatalf("expected no extra running worker in single-node mode, got %d", running)
	}
	if len(errCh) != 0 {
		t.Fatalf("expected no elector error in single-node mode")
	}
	if supervisor.startedTerms() != "1" {
		t.Fatalf("expected single-node start term 1, got %q", supervisor.startedTerms())
	}
}

func TestApplication_StartCoordinatorLeaderElectionBridgesCallbacks(t *testing.T) {
	supervisor := &stubCoordinatorSupervisor{}
	elector := stubLeaderElector{
		run: func(ctx context.Context, callbacks leaderpkg.Callbacks) error {
			leaderCtx, cancel := context.WithCancel(ctx)
			callbacks.OnStartedLeading(leaderCtx, 42)
			cancel()
			callbacks.OnStoppedLeading(42)
			return nil
		},
	}
	app := &application{
		supervisor: supervisor,
		elector:    elector,
	}
	errCh := make(chan error, 1)

	if running := app.startCoordinator(context.Background(), errCh); running != 1 {
		t.Fatalf("expected one running elector worker, got %d", running)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected elector error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for elector to finish")
	}

	if supervisor.startedTerms() != "42" {
		t.Fatalf("expected started term 42, got %q", supervisor.startedTerms())
	}
	if supervisor.stoppedTerms() != "42" {
		t.Fatalf("expected stopped term 42, got %q", supervisor.stoppedTerms())
	}
}

type stubCoordinatorSupervisor struct {
	mu      sync.Mutex
	started []int64
	stopped []int64
}

func (s *stubCoordinatorSupervisor) StartLeading(ctx context.Context, term int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = append(s.started, term)
}

func (s *stubCoordinatorSupervisor) StopLeading(term int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = append(s.stopped, term)
}

func (s *stubCoordinatorSupervisor) startedTerms() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return joinTerms(s.started)
}

func (s *stubCoordinatorSupervisor) stoppedTerms() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return joinTerms(s.stopped)
}

type stubLeaderElector struct {
	run func(ctx context.Context, callbacks leaderpkg.Callbacks) error
}

type stubTaskProducer struct{}

func (s stubLeaderElector) Run(ctx context.Context, callbacks leaderpkg.Callbacks) error {
	if s.run == nil {
		return nil
	}
	return s.run(ctx, callbacks)
}

func (stubTaskProducer) PublishReplicaRepair(ctx context.Context, task contracts.ReplicaRepairTask) error {
	return nil
}

func (stubTaskProducer) PublishCleanup(ctx context.Context, task contracts.CleanupTask) error {
	return nil
}

func (stubTaskProducer) PublishRebalance(ctx context.Context, task contracts.RebalanceTask) error {
	return nil
}

func (stubTaskProducer) PublishFailover(ctx context.Context, task contracts.FailoverTask) error {
	return nil
}

func joinTerms(terms []int64) string {
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		parts = append(parts, fmt.Sprintf("%d", term))
	}
	return strings.Join(parts, ",")
}
