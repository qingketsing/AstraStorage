package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	rabbitmqclient "AstraStorage/internal/platform/mq/rabbitmq/client"
	pgclient "AstraStorage/internal/platform/postgres/client"
	redisclient "AstraStorage/internal/platform/redis/client"
)

type Backend string

const (
	BackendMemory   Backend = "memory"
	BackendPostgres Backend = "postgres"
)

// HTTPConfig 描述 MDS 对外 HTTP 服务的最小运行配置。
type HTTPConfig struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
}

// GRPCConfig 描述 MDS gRPC 服务的最小运行配置。
type GRPCConfig struct {
	Addr string
}

// RepairConfig 描述 MDS 后台副本修复任务的运行配置。
type RepairConfig struct {
	Interval          time.Duration
	HTTPTimeout       time.Duration
	RetryBackoff      time.Duration
	MaxReplicasPerRun int
}

type FailoverConfig struct {
	Interval       time.Duration
	NodeTimeout    time.Duration
	MaxPlansPerRun int
}

type CleanupConfig struct {
	Interval       time.Duration
	HTTPTimeout    time.Duration
	RetryBackoff   time.Duration
	MaxPlansPerRun int
}

type RebalanceConfig struct {
	Interval       time.Duration
	HighWatermark  float64
	LowWatermark   float64
	MaxPlansPerRun int
}

type RabbitMQConfig struct {
	Enabled bool
	rabbitmqclient.Config
}

// LeaderElectionConfig 描述 MDS 控制面选主配置。
type LeaderElectionConfig struct {
	Enabled       bool
	InstanceID    string
	Prefix        string
	LeaseTTL      time.Duration
	EtcdEndpoints []string
	DialTimeout   time.Duration
}

// WithDefaults 为 HTTP 服务补齐默认值。
func (c HTTPConfig) WithDefaults() HTTPConfig {
	if strings.TrimSpace(c.Addr) == "" {
		c.Addr = ":8080"
	}
	if c.ReadHeaderTimeout <= 0 {
		c.ReadHeaderTimeout = 5 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 10 * time.Second
	}
	return c
}

// Validate 校验 HTTP 配置是否合法。
func (c HTTPConfig) Validate() error {
	if strings.TrimSpace(c.Addr) == "" {
		return fmt.Errorf("mds config: http addr is required")
	}
	if c.ReadHeaderTimeout < 0 {
		return fmt.Errorf("mds config: read header timeout cannot be negative")
	}
	if c.ShutdownTimeout < 0 {
		return fmt.Errorf("mds config: shutdown timeout cannot be negative")
	}
	return nil
}

// WithDefaults 为后台修复任务补齐默认值。
func (c RepairConfig) WithDefaults() RepairConfig {
	if c.Interval <= 0 {
		c.Interval = 15 * time.Second
	}
	if c.HTTPTimeout <= 0 {
		c.HTTPTimeout = 5 * time.Second
	}
	if c.RetryBackoff <= 0 {
		c.RetryBackoff = 30 * time.Second
	}
	if c.MaxReplicasPerRun <= 0 {
		c.MaxReplicasPerRun = 32
	}
	return c
}

// Validate 校验后台修复配置是否合法。
func (c RepairConfig) Validate() error {
	if c.Interval < 0 {
		return fmt.Errorf("mds config: repair interval cannot be negative")
	}
	if c.HTTPTimeout < 0 {
		return fmt.Errorf("mds config: repair http timeout cannot be negative")
	}
	if c.RetryBackoff < 0 {
		return fmt.Errorf("mds config: repair retry backoff cannot be negative")
	}
	if c.MaxReplicasPerRun < 0 {
		return fmt.Errorf("mds config: repair max replicas per run cannot be negative")
	}
	return nil
}

func (c FailoverConfig) WithDefaults() FailoverConfig {
	if c.Interval <= 0 {
		c.Interval = 15 * time.Second
	}
	if c.NodeTimeout <= 0 {
		c.NodeTimeout = 45 * time.Second
	}
	if c.MaxPlansPerRun <= 0 {
		c.MaxPlansPerRun = 32
	}
	return c
}

func (c FailoverConfig) Validate() error {
	if c.Interval < 0 {
		return fmt.Errorf("mds config: failover interval cannot be negative")
	}
	if c.NodeTimeout < 0 {
		return fmt.Errorf("mds config: failover node timeout cannot be negative")
	}
	if c.MaxPlansPerRun < 0 {
		return fmt.Errorf("mds config: failover max plans per run cannot be negative")
	}
	return nil
}

func (c CleanupConfig) WithDefaults() CleanupConfig {
	if c.Interval <= 0 {
		c.Interval = 15 * time.Second
	}
	if c.HTTPTimeout <= 0 {
		c.HTTPTimeout = 5 * time.Second
	}
	if c.RetryBackoff <= 0 {
		c.RetryBackoff = 30 * time.Second
	}
	if c.MaxPlansPerRun <= 0 {
		c.MaxPlansPerRun = 32
	}
	return c
}

func (c CleanupConfig) Validate() error {
	if c.Interval < 0 {
		return fmt.Errorf("mds config: cleanup interval cannot be negative")
	}
	if c.HTTPTimeout < 0 {
		return fmt.Errorf("mds config: cleanup http timeout cannot be negative")
	}
	if c.RetryBackoff < 0 {
		return fmt.Errorf("mds config: cleanup retry backoff cannot be negative")
	}
	if c.MaxPlansPerRun < 0 {
		return fmt.Errorf("mds config: cleanup max plans per run cannot be negative")
	}
	return nil
}

func (c RebalanceConfig) WithDefaults() RebalanceConfig {
	if c.Interval <= 0 {
		c.Interval = 30 * time.Second
	}
	if c.HighWatermark <= 0 {
		c.HighWatermark = 0.85
	}
	if c.LowWatermark <= 0 {
		c.LowWatermark = 0.60
	}
	if c.MaxPlansPerRun <= 0 {
		c.MaxPlansPerRun = 32
	}
	return c
}

func (c RebalanceConfig) Validate() error {
	if c.Interval < 0 {
		return fmt.Errorf("mds config: rebalance interval cannot be negative")
	}
	if c.HighWatermark <= 0 || c.HighWatermark > 1 {
		return fmt.Errorf("mds config: rebalance high watermark must be in (0,1]")
	}
	if c.LowWatermark < 0 || c.LowWatermark > 1 {
		return fmt.Errorf("mds config: rebalance low watermark must be in [0,1]")
	}
	if c.LowWatermark > c.HighWatermark {
		return fmt.Errorf("mds config: rebalance low watermark cannot exceed high watermark")
	}
	if c.MaxPlansPerRun < 0 {
		return fmt.Errorf("mds config: rebalance max plans per run cannot be negative")
	}
	return nil
}

func (c RabbitMQConfig) WithDefaults() RabbitMQConfig {
	c.Config = c.Config.WithDefaults()
	return c
}

func (c RabbitMQConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	return c.Config.WithDefaults().Validate()
}

// WithDefaults 为选主配置补齐默认值。
func (c LeaderElectionConfig) WithDefaults() LeaderElectionConfig {
	if strings.TrimSpace(c.Prefix) == "" {
		c.Prefix = "/astrastorage/controlplane/mds/leader"
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = 10 * time.Second
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = 5 * time.Second
	}
	return c
}

// Validate 校验选主配置是否合法。
func (c LeaderElectionConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if len(c.EtcdEndpoints) == 0 {
		return fmt.Errorf("mds config: MDS_ETCD_ENDPOINTS is required when leader election is enabled")
	}
	if strings.TrimSpace(c.Prefix) == "" {
		return fmt.Errorf("mds config: leader election prefix is required")
	}
	if c.LeaseTTL <= 0 {
		return fmt.Errorf("mds config: leader lease ttl must be positive")
	}
	if c.DialTimeout <= 0 {
		return fmt.Errorf("mds config: etcd dial timeout must be positive")
	}
	return nil
}

// Config 描述 MDS 当前所需的最小运行配置。
type Config struct {
	Backend        Backend
	HTTP           HTTPConfig
	GRPC           GRPCConfig
	Repair         RepairConfig
	Failover       FailoverConfig
	Cleanup        CleanupConfig
	Rebalance      RebalanceConfig
	LeaderElection LeaderElectionConfig
	Postgres       pgclient.Config
	Redis          RedisConfig
	RabbitMQ       RabbitMQConfig
}

type RedisConfig = redisclient.Config
type RedisReplicationGroupConfig = redisclient.ReplicationGroupConfig
type RedisWarmupConfig = redisclient.WarmupConfig

// LoadFromEnv 从环境变量读取 MDS 配置。
func LoadFromEnv() (Config, error) {
	cfg := Config{
		Backend:        BackendMemory,
		HTTP:           HTTPConfig{}.WithDefaults(),
		Repair:         RepairConfig{}.WithDefaults(),
		Failover:       FailoverConfig{}.WithDefaults(),
		Cleanup:        CleanupConfig{}.WithDefaults(),
		Rebalance:      RebalanceConfig{}.WithDefaults(),
		LeaderElection: LeaderElectionConfig{}.WithDefaults(),
	}
	if raw := strings.TrimSpace(os.Getenv("MDS_STORE_BACKEND")); raw != "" {
		cfg.Backend = Backend(strings.ToLower(raw))
	}
	if raw := strings.TrimSpace(os.Getenv("MDS_HTTP_ADDR")); raw != "" {
		cfg.HTTP.Addr = raw
	}
	cfg.GRPC.Addr = strings.TrimSpace(os.Getenv("MDS_GRPC_ADDR"))
	if value, ok, err := envDuration("MDS_HTTP_READ_HEADER_TIMEOUT"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.HTTP.ReadHeaderTimeout = value
	}
	if value, ok, err := envDuration("MDS_HTTP_SHUTDOWN_TIMEOUT"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.HTTP.ShutdownTimeout = value
	}
	cfg.HTTP = cfg.HTTP.WithDefaults()
	if value, ok, err := envDuration("MDS_REPAIR_INTERVAL"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Repair.Interval = value
	}
	if value, ok, err := envDuration("MDS_REPAIR_HTTP_TIMEOUT"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Repair.HTTPTimeout = value
	}
	if value, ok, err := envDuration("MDS_REPAIR_RETRY_BACKOFF"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Repair.RetryBackoff = value
	}
	if value, ok, err := envInt("MDS_REPAIR_MAX_REPLICAS_PER_RUN"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Repair.MaxReplicasPerRun = value
	}
	cfg.Repair = cfg.Repair.WithDefaults()
	if value, ok, err := envDuration("MDS_FAILOVER_INTERVAL"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Failover.Interval = value
	}
	if value, ok, err := envDuration("MDS_FAILOVER_NODE_TIMEOUT"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Failover.NodeTimeout = value
	}
	if value, ok, err := envInt("MDS_FAILOVER_MAX_PLANS_PER_RUN"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Failover.MaxPlansPerRun = value
	}
	cfg.Failover = cfg.Failover.WithDefaults()

	if value, ok, err := envDuration("MDS_CLEANUP_INTERVAL"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Cleanup.Interval = value
	}
	if value, ok, err := envDuration("MDS_CLEANUP_HTTP_TIMEOUT"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Cleanup.HTTPTimeout = value
	}
	if value, ok, err := envDuration("MDS_CLEANUP_RETRY_BACKOFF"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Cleanup.RetryBackoff = value
	}
	if value, ok, err := envInt("MDS_CLEANUP_MAX_PLANS_PER_RUN"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Cleanup.MaxPlansPerRun = value
	}
	cfg.Cleanup = cfg.Cleanup.WithDefaults()

	if value, ok, err := envDuration("MDS_REBALANCE_INTERVAL"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Rebalance.Interval = value
	}
	if value, ok, err := envFloat64("MDS_REBALANCE_HIGH_WATERMARK"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Rebalance.HighWatermark = value
	}
	if value, ok, err := envFloat64("MDS_REBALANCE_LOW_WATERMARK"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Rebalance.LowWatermark = value
	}
	if value, ok, err := envInt("MDS_REBALANCE_MAX_PLANS_PER_RUN"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Rebalance.MaxPlansPerRun = value
	}
	cfg.Rebalance = cfg.Rebalance.WithDefaults()

	if value, ok, err := envBool("MDS_LEADER_ELECTION_ENABLED"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.LeaderElection.Enabled = value
	}
	if raw := strings.TrimSpace(os.Getenv("MDS_ETCD_ENDPOINTS")); raw != "" {
		cfg.LeaderElection.EtcdEndpoints = splitCSV(raw)
	}
	if value, ok, err := envDuration("MDS_ETCD_DIAL_TIMEOUT"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.LeaderElection.DialTimeout = value
	}
	if raw := strings.TrimSpace(os.Getenv("MDS_LEADER_ELECTION_PREFIX")); raw != "" {
		cfg.LeaderElection.Prefix = raw
	}
	if value, ok, err := envDuration("MDS_LEADER_LEASE_TTL"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.LeaderElection.LeaseTTL = value
	}
	if raw := strings.TrimSpace(os.Getenv("MDS_INSTANCE_ID")); raw != "" {
		cfg.LeaderElection.InstanceID = raw
	}
	cfg.LeaderElection = cfg.LeaderElection.WithDefaults()

	if value, ok, err := envBool("MDS_REDIS_ENABLED"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Enabled = value
	}
	if raw := strings.TrimSpace(os.Getenv("MDS_REDIS_SENTINEL_ENDPOINTS")); raw != "" {
		cfg.Redis.SentinelEndpoints = splitCSV(raw)
	}
	if raw := strings.TrimSpace(os.Getenv("MDS_REDIS_USERNAME")); raw != "" {
		cfg.Redis.Username = raw
	}
	if raw := strings.TrimSpace(os.Getenv("MDS_REDIS_PASSWORD")); raw != "" {
		cfg.Redis.Password = raw
	}
	if raw := strings.TrimSpace(os.Getenv("MDS_REDIS_CACHE_MASTER_SET")); raw != "" {
		cfg.Redis.Cache.MasterSetName = raw
	}
	if raw := strings.TrimSpace(os.Getenv("MDS_REDIS_COORD_MASTER_SET")); raw != "" {
		cfg.Redis.Coord.MasterSetName = raw
	}
	if value, ok, err := envDuration("MDS_REDIS_DIAL_TIMEOUT"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.DialTimeout = value
	}
	if value, ok, err := envDuration("MDS_REDIS_READ_TIMEOUT"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.ReadTimeout = value
	}
	if value, ok, err := envDuration("MDS_REDIS_WRITE_TIMEOUT"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.WriteTimeout = value
	}
	if value, ok, err := envDuration("MDS_REDIS_FILE_META_TTL"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Cache.FileMetaTTL = value
	}
	if value, ok, err := envDuration("MDS_REDIS_FILE_META_TTL_JITTER"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Cache.FileMetaTTLJitter = value
	}
	if value, ok, err := envDuration("MDS_REDIS_DOWNLOAD_PLAN_TTL"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Cache.DownloadPlanTTL = value
	}
	if value, ok, err := envDuration("MDS_REDIS_DIRECTORY_LIST_TTL"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Cache.DirectoryListTTL = value
	}
	if value, ok, err := envDuration("MDS_REDIS_NODE_HEALTH_TTL"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Cache.NodeHealthTTL = value
	}
	if value, ok, err := envDuration("MDS_REDIS_NULL_ENTRY_TTL"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Cache.NullEntryTTL = value
	}
	if value, ok, err := envInt("MDS_REDIS_HOTSPOT_THRESHOLD"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Cache.HotspotThreshold = value
	}
	if value, ok, err := envDuration("MDS_REDIS_HOTSPOT_WINDOW"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Cache.HotspotWindow = value
	}
	if value, ok, err := envDuration("MDS_REDIS_STALE_SERVE_WINDOW"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Cache.StaleServeWindow = value
	}
	if value, ok, err := envInt("MDS_REDIS_BLOOM_EXPECTED_INSERTIONS"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Cache.BloomExpectedInsert = value
	}
	if value, ok, err := envFloat64("MDS_REDIS_BLOOM_FALSE_POSITIVE"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Cache.BloomFalsePositive = value
	}
	if value, ok, err := envDuration("MDS_REDIS_WARMUP_INTERVAL"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Warmup.Interval = value
	}
	if value, ok, err := envInt("MDS_REDIS_WARMUP_BATCH_SIZE"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Warmup.BatchSize = value
	}
	if value, ok, err := envInt("MDS_REDIS_WARMUP_CONCURRENCY"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Warmup.Concurrency = value
	}
	if value, ok, err := envDuration("MDS_REDIS_WARMUP_LOCK_TTL"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Warmup.LockTTL = value
	}
	if value, ok, err := envInt("MDS_REDIS_WARMUP_STARTUP_TOP_N"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Warmup.StartupTopN = value
	}
	if value, ok, err := envDuration("MDS_REDIS_HOTSET_REFRESH_INTERVAL"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Redis.Warmup.HotsetRefresh = value
	}
	cfg.Redis = cfg.Redis.WithDefaults()

	if value, ok, err := envBool("MDS_RABBITMQ_ENABLED"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.RabbitMQ.Enabled = value
	}
	if raw := strings.TrimSpace(os.Getenv("MDS_RABBITMQ_ENDPOINTS")); raw != "" {
		cfg.RabbitMQ.Endpoints = splitCSV(raw)
	}
	if raw := strings.TrimSpace(os.Getenv("MDS_RABBITMQ_USERNAME")); raw != "" {
		cfg.RabbitMQ.Username = raw
	}
	if raw := strings.TrimSpace(os.Getenv("MDS_RABBITMQ_PASSWORD")); raw != "" {
		cfg.RabbitMQ.Password = raw
	}
	if raw := strings.TrimSpace(os.Getenv("MDS_RABBITMQ_VHOST")); raw != "" {
		cfg.RabbitMQ.VHost = raw
	}
	if value, ok, err := envDuration("MDS_RABBITMQ_CONNECTION_TIMEOUT"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.RabbitMQ.ConnectionTimeout = value
	}
	if value, ok, err := envDuration("MDS_RABBITMQ_HEARTBEAT"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.RabbitMQ.Heartbeat = value
	}
	if value, ok, err := envInt("MDS_RABBITMQ_CONSUMER_PREFETCH"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.RabbitMQ.ConsumerPrefetch = value
	}
	if value, ok, err := envBool("MDS_RABBITMQ_PUBLISHER_CONFIRM"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.RabbitMQ.PublisherConfirm = value
	}
	cfg.RabbitMQ = cfg.RabbitMQ.WithDefaults()

	cfg.Postgres.DSN = strings.TrimSpace(os.Getenv("MDS_POSTGRES_DSN"))
	if value, ok, err := envInt32("MDS_POSTGRES_MAX_CONNS"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Postgres.MaxConns = value
	}
	if value, ok, err := envInt32("MDS_POSTGRES_MIN_CONNS"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Postgres.MinConns = value
	}
	if value, ok, err := envDuration("MDS_POSTGRES_CONNECT_TIMEOUT"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Postgres.ConnectTimeout = value
	}
	if value, ok, err := envDuration("MDS_POSTGRES_HEALTHCHECK_PERIOD"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Postgres.HealthCheckPeriod = value
	}
	if value, ok, err := envDuration("MDS_POSTGRES_MAX_CONN_IDLE_TIME"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Postgres.MaxConnIdleTime = value
	}
	if value, ok, err := envDuration("MDS_POSTGRES_MAX_CONN_LIFETIME"); err != nil {
		return Config{}, err
	} else if ok {
		cfg.Postgres.MaxConnLifetime = value
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate 校验配置是否合法。
func (c Config) Validate() error {
	if err := c.HTTP.WithDefaults().Validate(); err != nil {
		return err
	}
	if err := c.Repair.WithDefaults().Validate(); err != nil {
		return err
	}
	if err := c.Failover.WithDefaults().Validate(); err != nil {
		return err
	}
	if err := c.Cleanup.WithDefaults().Validate(); err != nil {
		return err
	}
	if err := c.Rebalance.WithDefaults().Validate(); err != nil {
		return err
	}
	if err := c.LeaderElection.WithDefaults().Validate(); err != nil {
		return err
	}
	if err := c.Redis.WithDefaults().Validate(); err != nil {
		return err
	}
	if err := c.RabbitMQ.WithDefaults().Validate(); err != nil {
		return err
	}
	if c.GRPC.Addr != "" && strings.TrimSpace(c.GRPC.Addr) == "" {
		return fmt.Errorf("mds config: grpc addr is invalid")
	}
	switch c.Backend {
	case BackendMemory:
		return nil
	case BackendPostgres:
		if c.Postgres.DSN == "" {
			return fmt.Errorf("mds config: MDS_POSTGRES_DSN is required when backend=postgres")
		}
		return c.Postgres.WithDefaults().Validate()
	default:
		return fmt.Errorf("mds config: unsupported backend %q", c.Backend)
	}
}

func envInt32(name string) (int32, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, false, fmt.Errorf("mds config: parse %s: %w", name, err)
	}
	return int32(value), true, nil
}

func envInt(name string) (int, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false, fmt.Errorf("mds config: parse %s: %w", name, err)
	}
	return value, true, nil
}

func envDuration(name string) (time.Duration, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false, fmt.Errorf("mds config: parse %s: %w", name, err)
	}
	return value, true, nil
}

func envBool(name string) (bool, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false, false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false, fmt.Errorf("mds config: parse %s: %w", name, err)
	}
	return value, true, nil
}

func envFloat64(name string) (float64, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false, fmt.Errorf("mds config: parse %s: %w", name, err)
	}
	return value, true, nil
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
