package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"AstraStorage/internal/mds"
	mdsconfig "AstraStorage/internal/mds/config"
	"AstraStorage/internal/mds/coordinator"
	"AstraStorage/internal/mds/metadata"
	mdsmq "AstraStorage/internal/mds/mq"
	mdsrpc "AstraStorage/internal/mds/rpc"
	"AstraStorage/internal/mds/store"
	etcdclient "AstraStorage/internal/platform/etcd/client"
	leaderpkg "AstraStorage/internal/platform/etcd/leader"
	rabbitmqclient "AstraStorage/internal/platform/mq/rabbitmq/client"
	idempotencypkg "AstraStorage/internal/platform/mq/rabbitmq/idempotency"
	"AstraStorage/internal/platform/observability/logging"
	"AstraStorage/internal/platform/observability/metrics"
	pgclient "AstraStorage/internal/platform/postgres/client"
	pgmigrate "AstraStorage/internal/platform/postgres/migrate"
	pgrepository "AstraStorage/internal/platform/postgres/repository"
	redisclient "AstraStorage/internal/platform/redis/client"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
)

// application 描述当前 MDS 进程已经组装好的核心组件。
// 现在先保留最小依赖链，后续可以继续挂配置、server 和后台任务。
type application struct {
	repo              store.Repository
	service           *mds.Service
	handler           *mds.Handler
	router            *mdsrpc.Router
	observability     *mds.Observability
	repairer          *coordinator.PendingReplicaRepairer
	failoverPlanner   *coordinator.FailoverPlanner
	cleanupController *coordinator.CleanupController
	rebalancePlanner  *coordinator.RebalancePlanner
	supervisor        coordinatorSupervisor
	warmup            cacheWarmupRunner
	elector           leaderElector
	leaderLogger      *slog.Logger
	httpServer        *http.Server
	httpAddr          string
	grpcServer        *grpc.Server
	grpcAddr          string
	redis             *redisclient.Bundle
	rabbitmq          *rabbitmqclient.Manager
	taskProducer      mdsmq.TaskProducer
	consumers         rabbitMQConsumerOrchestrator
	shutdownTimeout   time.Duration
	closeFn           func()

	leaderStateMu sync.Mutex
	leaderActive  bool
	leaderTerm    int64
}

type coordinatorSupervisor interface {
	StartLeading(ctx context.Context, term int64)
	StopLeading(term int64)
}

type leaderElector interface {
	Run(ctx context.Context, callbacks leaderpkg.Callbacks) error
}

type cacheWarmupRunner interface {
	Run(ctx context.Context) error
}

type rabbitMQConsumerOrchestrator interface {
	Run(ctx context.Context) error
}

var etcdClientFactory = func(cfg mdsconfig.LeaderElectionConfig) (*clientv3.Client, error) {
	clientCfg, err := etcdclient.NewConfig(cfg.EtcdEndpoints, cfg.DialTimeout)
	if err != nil {
		return nil, err
	}
	return etcdclient.New(clientCfg)
}

var leaderElectorFactory = func(client *clientv3.Client, cfg mdsconfig.LeaderElectionConfig) (leaderElector, error) {
	return leaderpkg.New(client, leaderpkg.Config{
		Prefix:     cfg.Prefix,
		InstanceID: instanceIDForLeaderElection(cfg.InstanceID),
		LeaseTTL:   cfg.LeaseTTL,
	})
}

var redisBundleFactory = func(cfg mdsconfig.RedisConfig) (*redisclient.Bundle, error) {
	return redisclient.NewBundle(cfg)
}

var redisWarmupRunnerFactory = func(service *mds.Service, bundle *redisclient.Bundle, cfg mdsconfig.RedisWarmupConfig) cacheWarmupRunner {
	return mds.NewRedisWarmupRunner(service, bundle, cfg)
}

var rabbitMQManagerFactory = func(cfg mdsconfig.RabbitMQConfig) (*rabbitmqclient.Manager, error) {
	return rabbitmqclient.NewManager(cfg.Config)
}

var rabbitMQTaskProducerFactory = func(manager *rabbitmqclient.Manager) (mdsmq.TaskProducer, error) {
	return mdsmq.NewRabbitMQTaskProducer(manager)
}

var rabbitMQConsumerOrchestratorFactory = func(manager *rabbitmqclient.Manager, prefetch int, repairer *coordinator.PendingReplicaRepairer, cleanup *coordinator.CleanupController) rabbitMQConsumerOrchestrator {
	idempotencyHandler := idempotencypkg.NewHandler(idempotencypkg.NewMemoryStore(), 10*time.Minute)
	repair := mdsmq.NewRepairConsumer(repairer)
	repair.SetIdempotencyHandler(idempotencyHandler)
	cleanupConsumer := mdsmq.NewCleanupConsumer(cleanup)
	cleanupConsumer.SetIdempotencyHandler(idempotencyHandler)
	rebalance := mdsmq.NewRebalanceConsumer(repairer)
	rebalance.SetIdempotencyHandler(idempotencyHandler)
	failover := mdsmq.NewFailoverConsumer(repairer)
	failover.SetIdempotencyHandler(idempotencyHandler)
	return mdsmq.NewOrchestrator(
		manager,
		prefetch,
		repair,
		cleanupConsumer,
		rebalance,
		failover,
	)
}

func newApplication(ctx context.Context) (*application, error) {
	cfg, err := mdsconfig.LoadFromEnv()
	if err != nil {
		return nil, err
	}
	return newApplicationWithConfig(ctx, cfg)
}

func newApplicationWithConfig(ctx context.Context, cfg mdsconfig.Config) (*application, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.LeaderElection = cfg.LeaderElection.WithDefaults()
	repo, closeFn, err := newRepository(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := ensureRootInode(ctx, repo, time.Now().UTC()); err != nil {
		if closeFn != nil {
			closeFn()
		}
		return nil, err
	}

	service, err := mds.NewService(repo)
	if err != nil {
		if closeFn != nil {
			closeFn()
		}
		return nil, err
	}
	handler, err := mds.NewHandler(service)
	if err != nil {
		if closeFn != nil {
			closeFn()
		}
		return nil, err
	}
	registry := metrics.NewRegistry("mds")
	observability, err := mds.NewObservability(registry)
	if err != nil {
		if closeFn != nil {
			closeFn()
		}
		return nil, err
	}
	handler.SetObservability(observability)
	router, err := mdsrpc.NewRouter(handler)
	if err != nil {
		if closeFn != nil {
			closeFn()
		}
		return nil, err
	}
	httpHandler, err := mdsrpc.NewHTTPHandler(router, repo, registry)
	if err != nil {
		if closeFn != nil {
			closeFn()
		}
		return nil, err
	}
	httpCfg := cfg.HTTP.WithDefaults()
	httpServer := &http.Server{
		Addr:              httpCfg.Addr,
		Handler:           httpHandler,
		ReadHeaderTimeout: httpCfg.ReadHeaderTimeout,
	}
	var grpcServer *grpc.Server
	if cfg.GRPC.Addr != "" {
		grpcServer, err = mdsrpc.NewGRPCServer(router, repo)
		if err != nil {
			if closeFn != nil {
				closeFn()
			}
			return nil, err
		}
	}
	var redisBundle *redisclient.Bundle
	var warmupRunner cacheWarmupRunner
	var rabbitManager *rabbitmqclient.Manager
	var taskProducer mdsmq.TaskProducer
	var consumerOrchestrator rabbitMQConsumerOrchestrator
	if cfg.Redis.Enabled {
		redisBundle, err = redisBundleFactory(cfg.Redis)
		if err != nil {
			if closeFn != nil {
				closeFn()
			}
			return nil, err
		}
		previousClose := closeFn
		closeFn = func() {
			if previousClose != nil {
				previousClose()
			}
			_ = redisBundle.Close()
		}
	}
	if redisBundle != nil {
		service.SetReadCache(mds.NewRedisReadCache(redisBundle, cfg.Redis.Cache, cfg.Redis.Warmup.LockTTL))
		warmupRunner = redisWarmupRunnerFactory(service, redisBundle, cfg.Redis.Warmup)
	}
	if cfg.RabbitMQ.Enabled {
		rabbitManager, err = rabbitMQManagerFactory(cfg.RabbitMQ)
		if err != nil {
			if closeFn != nil {
				closeFn()
			}
			return nil, err
		}
		taskProducer, err = rabbitMQTaskProducerFactory(rabbitManager)
		if err != nil {
			_ = rabbitManager.Close()
			if closeFn != nil {
				closeFn()
			}
			return nil, err
		}
		previousClose := closeFn
		closeFn = func() {
			if producerCloser, ok := taskProducer.(interface{ Close() error }); ok {
				_ = producerCloser.Close()
			}
			if rabbitManager != nil {
				_ = rabbitManager.Close()
			}
			if previousClose != nil {
				previousClose()
			}
		}
	}
	repairer, err := coordinator.NewPendingReplicaRepairer(repo, coordinator.PendingReplicaRepairerConfig{
		Interval:          cfg.Repair.Interval,
		HTTPTimeout:       cfg.Repair.HTTPTimeout,
		RetryBackoff:      cfg.Repair.RetryBackoff,
		MaxReplicasPerRun: cfg.Repair.MaxReplicasPerRun,
	})
	if err != nil {
		if closeFn != nil {
			closeFn()
		}
		return nil, err
	}
	repairer.SetObservability(observability)
	if taskProducer != nil {
		repairer.SetTaskProducer(taskProducer)
	}
	failoverPlanner := coordinator.NewFailoverPlanner(repo, coordinator.FailoverPlannerConfig{
		Interval:       cfg.Failover.Interval,
		NodeTimeout:    cfg.Failover.NodeTimeout,
		MaxPlansPerRun: cfg.Failover.MaxPlansPerRun,
	})
	if taskProducer != nil {
		failoverPlanner.SetTaskProducer(taskProducer)
	}
	cleanupController := coordinator.NewCleanupController(repo, coordinator.CleanupControllerConfig{
		Interval:       cfg.Cleanup.Interval,
		HTTPTimeout:    cfg.Cleanup.HTTPTimeout,
		RetryBackoff:   cfg.Cleanup.RetryBackoff,
		MaxPlansPerRun: cfg.Cleanup.MaxPlansPerRun,
	})
	if taskProducer != nil {
		cleanupController.SetTaskProducer(taskProducer)
	}
	rebalancePlanner := coordinator.NewRebalancePlanner(repo, coordinator.RebalancePlannerConfig{
		Interval:       cfg.Rebalance.Interval,
		HighWatermark:  cfg.Rebalance.HighWatermark,
		LowWatermark:   cfg.Rebalance.LowWatermark,
		MaxPlansPerRun: cfg.Rebalance.MaxPlansPerRun,
	})
	if taskProducer != nil {
		rebalancePlanner.SetTaskProducer(taskProducer)
	}
	if rabbitManager != nil {
		consumerOrchestrator = rabbitMQConsumerOrchestratorFactory(rabbitManager, cfg.RabbitMQ.ConsumerPrefetch, repairer, cleanupController)
	}

	var supervisor coordinatorSupervisor
	if repairer != nil || failoverPlanner != nil || cleanupController != nil || rebalancePlanner != nil {
		supervisor = coordinator.NewSupervisor(repairer, failoverPlanner, cleanupController, rebalancePlanner)
	}

	var elector leaderElector
	if cfg.LeaderElection.Enabled {
		client, err := etcdClientFactory(cfg.LeaderElection)
		if err != nil {
			if closeFn != nil {
				closeFn()
			}
			return nil, err
		}
		elector, err = leaderElectorFactory(client, cfg.LeaderElection)
		if err != nil {
			_ = client.Close()
			if closeFn != nil {
				closeFn()
			}
			return nil, err
		}
		previousClose := closeFn
		closeFn = func() {
			if previousClose != nil {
				previousClose()
			}
			_ = client.Close()
		}
	}

	return &application{
		repo:              repo,
		service:           service,
		handler:           handler,
		router:            router,
		observability:     observability,
		repairer:          repairer,
		failoverPlanner:   failoverPlanner,
		cleanupController: cleanupController,
		rebalancePlanner:  rebalancePlanner,
		supervisor:        supervisor,
		warmup:            warmupRunner,
		elector:           elector,
		leaderLogger:      logging.NewLogger(os.Stderr, "mds", "leader"),
		httpServer:        httpServer,
		httpAddr:          httpCfg.Addr,
		grpcServer:        grpcServer,
		grpcAddr:          cfg.GRPC.Addr,
		redis:             redisBundle,
		rabbitmq:          rabbitManager,
		taskProducer:      taskProducer,
		consumers:         consumerOrchestrator,
		shutdownTimeout:   httpCfg.ShutdownTimeout,
		closeFn:           closeFn,
	}, nil
}

func newRepository(ctx context.Context, cfg mdsconfig.Config) (store.Repository, func(), error) {
	switch cfg.Backend {
	case mdsconfig.BackendMemory:
		return store.NewMemoryRepository(), nil, nil
	case mdsconfig.BackendPostgres:
		pool, err := pgclient.NewPool(ctx, cfg.Postgres)
		if err != nil {
			return nil, nil, err
		}
		migrator, err := pgmigrate.New()
		if err != nil {
			pool.Close()
			return nil, nil, err
		}
		if err := migrator.Up(ctx, pool); err != nil {
			pool.Close()
			return nil, nil, err
		}
		repo, err := pgrepository.New(pool)
		if err != nil {
			pool.Close()
			return nil, nil, err
		}
		return repo, pool.Close, nil
	default:
		return nil, nil, fmt.Errorf("mds bootstrap: unsupported backend %q", cfg.Backend)
	}
}

func (app *application) Close() {
	if app != nil && app.closeFn != nil {
		app.closeFn()
	}
}

func (app *application) Run(ctx context.Context) error {
	if app == nil || app.httpServer == nil {
		return errors.New("mds bootstrap: http server is nil")
	}

	errCh := make(chan error, 2)
	running := 0
	go func() {
		err := app.httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	running++

	var grpcListener net.Listener
	if app.grpcServer != nil && app.grpcAddr != "" {
		listener, err := net.Listen("tcp", app.grpcAddr)
		if err != nil {
			return fmt.Errorf("mds bootstrap: listen grpc %s: %w", app.grpcAddr, err)
		}
		grpcListener = listener
		go func() {
			if err := app.grpcServer.Serve(listener); err != nil {
				errCh <- err
				return
			}
			errCh <- nil
		}()
		running++
	}
	running += app.startCoordinator(ctx, errCh)
	running += app.startWarmup(ctx, errCh)
	running += app.startRabbitMQConsumers(ctx, errCh)

	for completed := 0; completed < running; {
		select {
		case <-ctx.Done():
			app.stopCoordinator()
			shutdownTimeout := app.shutdownTimeout
			if shutdownTimeout <= 0 {
				shutdownTimeout = 10 * time.Second
			}
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			if err := app.httpServer.Shutdown(shutdownCtx); err != nil {
				cancel()
				return fmt.Errorf("mds bootstrap: shutdown http server: %w", err)
			}
			cancel()
			if app.grpcServer != nil {
				app.grpcServer.GracefulStop()
			}
			if grpcListener != nil {
				_ = grpcListener.Close()
			}
			ctx = context.Background()
		case err := <-errCh:
			completed++
			if err != nil {
				app.stopCoordinator()
				if app.grpcServer != nil {
					app.grpcServer.Stop()
				}
				if grpcListener != nil {
					_ = grpcListener.Close()
				}
				_ = app.httpServer.Close()
				return fmt.Errorf("mds bootstrap: serve server: %w", err)
			}
		}
	}
	return nil
}

func (app *application) startWarmup(ctx context.Context, errCh chan<- error) int {
	if app == nil || app.warmup == nil {
		return 0
	}
	go func() {
		err := app.warmup.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	return 1
}

func (app *application) startRabbitMQConsumers(ctx context.Context, errCh chan<- error) int {
	if app == nil || app.consumers == nil {
		return 0
	}
	go func() {
		err := app.consumers.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	return 1
}

func (app *application) startCoordinator(ctx context.Context, errCh chan<- error) int {
	if app == nil || app.supervisor == nil {
		return 0
	}
	if app.elector == nil {
		app.onStartedLeading(ctx, 1)
		return 0
	}

	go func() {
		err := app.elector.Run(ctx, leaderpkg.Callbacks{
			OnStartedLeading: app.onStartedLeading,
			OnStoppedLeading: app.onStoppedLeading,
		})
		if err != nil && app.observability != nil {
			app.observability.RecordLeaderElectionFailure()
		}
		if err != nil && app.leaderLogger != nil {
			app.leaderLogger.Error("leader election exited with error", "error", err)
		}
		errCh <- err
	}()
	return 1
}

func (app *application) stopCoordinator() {
	if app == nil || app.supervisor == nil {
		return
	}
	app.onStoppedLeading(0)
}

func ensureRootInode(ctx context.Context, repo store.Repository, now time.Time) error {
	if repo == nil {
		return errors.New("mds bootstrap: repository is nil")
	}

	_, err := repo.GetInode(ctx, store.InodeSelector{ID: metadata.InodeID(metadata.RootInodeID)})
	if err == nil {
		return nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("mds bootstrap: check root inode: %w", err)
	}

	root := &metadata.InodeMetadata{
		ID:         metadata.InodeID(metadata.RootInodeID),
		Path:       "/",
		Type:       metadata.InodeTypeDirectory,
		Status:     metadata.InodeStatusActive,
		LinkCount:  1,
		Generation: 1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := repo.CreateInode(ctx, root); err != nil && !errors.Is(err, store.ErrAlreadyExists) {
		return fmt.Errorf("mds bootstrap: create root inode: %w", err)
	}
	return nil
}

func instanceIDForLeaderElection(configured string) string {
	if trimmed := strings.TrimSpace(configured); trimmed != "" {
		return trimmed
	}
	hostname, err := os.Hostname()
	if err == nil && strings.TrimSpace(hostname) != "" {
		return fmt.Sprintf("%s-%d", strings.TrimSpace(hostname), os.Getpid())
	}
	return fmt.Sprintf("mds-%d", os.Getpid())
}

func (app *application) onStartedLeading(ctx context.Context, term int64) {
	if app == nil || app.supervisor == nil {
		return
	}

	app.leaderStateMu.Lock()
	changed := !app.leaderActive || app.leaderTerm != term
	app.leaderActive = true
	app.leaderTerm = term
	app.leaderStateMu.Unlock()

	if changed && app.observability != nil {
		app.observability.RecordLeaderTransition("started")
		app.observability.SetLeaderState(true, term)
	}
	if changed && app.leaderLogger != nil {
		app.leaderLogger.Info("became leader", "term", term)
	}
	app.supervisor.StartLeading(ctx, term)
}

func (app *application) onStoppedLeading(term int64) {
	if app == nil || app.supervisor == nil {
		return
	}

	app.leaderStateMu.Lock()
	wasLeader := app.leaderActive
	app.leaderActive = false
	app.leaderTerm = 0
	app.leaderStateMu.Unlock()

	if wasLeader && app.observability != nil {
		app.observability.RecordLeaderTransition("stopped")
		app.observability.SetLeaderState(false, 0)
	}
	if wasLeader && app.leaderLogger != nil {
		app.leaderLogger.Info("lost leader", "term", term)
	}
	app.supervisor.StopLeading(term)
}
