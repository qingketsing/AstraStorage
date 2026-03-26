package client

import (
	"fmt"
	"strings"
	"time"
)

const (
	defaultDialTimeout       = 3 * time.Second
	defaultReadTimeout       = 1 * time.Second
	defaultWriteTimeout      = 1 * time.Second
	defaultFileMetaTTL       = 5 * time.Minute
	defaultFileMetaTTLJitter = 30 * time.Second
	defaultDownloadPlanTTL   = 3 * time.Minute
	defaultDirectoryListTTL  = 90 * time.Second
	defaultNodeHealthTTL     = 15 * time.Second
	defaultNullEntryTTL      = 30 * time.Second
	defaultHotspotThreshold  = 8
	defaultHotspotWindow     = time.Minute
	defaultStaleServeWindow  = 15 * time.Second
	defaultBloomExpected     = 100000
	defaultBloomFalsePos     = 0.01
	defaultWarmupInterval    = 30 * time.Second
	defaultWarmupBatchSize   = 32
	defaultWarmupConcurrency = 4
	defaultWarmupLockTTL     = 10 * time.Second
	defaultWarmupStartupTopN = 128
	defaultHotsetRefresh     = 2 * time.Minute
)

// Config describes the dual Redis replication-group topology managed by Sentinel.
type Config struct {
	Enabled           bool
	SentinelEndpoints []string
	Username          string
	Password          string
	DialTimeout       time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	Cache             ReplicationGroupConfig
	Coord             ReplicationGroupConfig
	Warmup            WarmupConfig
}

// ReplicationGroupConfig describes one logical Sentinel-managed master set.
type ReplicationGroupConfig struct {
	MasterSetName       string
	FileMetaTTL         time.Duration
	FileMetaTTLJitter   time.Duration
	DownloadPlanTTL     time.Duration
	DirectoryListTTL    time.Duration
	NodeHealthTTL       time.Duration
	NullEntryTTL        time.Duration
	HotspotThreshold    int
	HotspotWindow       time.Duration
	StaleServeWindow    time.Duration
	BloomExpectedInsert int
	BloomFalsePositive  float64
}

// WarmupConfig controls startup and background cache warmup behavior.
type WarmupConfig struct {
	Interval      time.Duration
	BatchSize     int
	Concurrency   int
	LockTTL       time.Duration
	StartupTopN   int
	HotsetRefresh time.Duration
}

func (c Config) WithDefaults() Config {
	if c.DialTimeout <= 0 {
		c.DialTimeout = defaultDialTimeout
	}
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = defaultReadTimeout
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = defaultWriteTimeout
	}
	c.Cache = c.Cache.withDefaults()
	c.Coord = c.Coord.withDefaults()
	c.Warmup = c.Warmup.withDefaults()
	return c
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if len(c.SentinelEndpoints) == 0 {
		return fmt.Errorf("redis config: sentinel endpoints are required when redis is enabled")
	}
	if strings.TrimSpace(c.Cache.MasterSetName) == "" {
		return fmt.Errorf("redis config: cache master set name is required")
	}
	if strings.TrimSpace(c.Coord.MasterSetName) == "" {
		return fmt.Errorf("redis config: coord master set name is required")
	}
	if c.Cache.MasterSetName == c.Coord.MasterSetName {
		return fmt.Errorf("redis config: cache and coord master set names must differ")
	}

	cfg := c.WithDefaults()
	if cfg.DialTimeout <= 0 {
		return fmt.Errorf("redis config: dial timeout must be positive")
	}
	if cfg.ReadTimeout <= 0 {
		return fmt.Errorf("redis config: read timeout must be positive")
	}
	if cfg.WriteTimeout <= 0 {
		return fmt.Errorf("redis config: write timeout must be positive")
	}
	if err := cfg.Cache.validate("cache"); err != nil {
		return err
	}
	if err := cfg.Coord.validate("coord"); err != nil {
		return err
	}
	if err := cfg.Warmup.validate(); err != nil {
		return err
	}
	return nil
}

func (c ReplicationGroupConfig) withDefaults() ReplicationGroupConfig {
	if c.FileMetaTTL <= 0 {
		c.FileMetaTTL = defaultFileMetaTTL
	}
	if c.FileMetaTTLJitter <= 0 {
		c.FileMetaTTLJitter = defaultFileMetaTTLJitter
	}
	if c.DownloadPlanTTL <= 0 {
		c.DownloadPlanTTL = defaultDownloadPlanTTL
	}
	if c.DirectoryListTTL <= 0 {
		c.DirectoryListTTL = defaultDirectoryListTTL
	}
	if c.NodeHealthTTL <= 0 {
		c.NodeHealthTTL = defaultNodeHealthTTL
	}
	if c.NullEntryTTL <= 0 {
		c.NullEntryTTL = defaultNullEntryTTL
	}
	if c.HotspotThreshold <= 0 {
		c.HotspotThreshold = defaultHotspotThreshold
	}
	if c.HotspotWindow <= 0 {
		c.HotspotWindow = defaultHotspotWindow
	}
	if c.StaleServeWindow <= 0 {
		c.StaleServeWindow = defaultStaleServeWindow
	}
	if c.BloomExpectedInsert <= 0 {
		c.BloomExpectedInsert = defaultBloomExpected
	}
	if c.BloomFalsePositive <= 0 {
		c.BloomFalsePositive = defaultBloomFalsePos
	}
	return c
}

func (c ReplicationGroupConfig) validate(name string) error {
	if strings.TrimSpace(c.MasterSetName) == "" {
		return fmt.Errorf("redis config: %s master set name is required", name)
	}
	cfg := c.withDefaults()
	if cfg.FileMetaTTL <= 0 || cfg.DownloadPlanTTL <= 0 || cfg.DirectoryListTTL <= 0 || cfg.NodeHealthTTL <= 0 || cfg.NullEntryTTL <= 0 {
		return fmt.Errorf("redis config: %s cache ttl values must be positive", name)
	}
	if cfg.FileMetaTTLJitter < 0 {
		return fmt.Errorf("redis config: %s ttl jitter cannot be negative", name)
	}
	if cfg.HotspotThreshold <= 0 {
		return fmt.Errorf("redis config: %s hotspot threshold must be positive", name)
	}
	if cfg.HotspotWindow <= 0 {
		return fmt.Errorf("redis config: %s hotspot window must be positive", name)
	}
	if cfg.StaleServeWindow < 0 {
		return fmt.Errorf("redis config: %s stale serve window cannot be negative", name)
	}
	if cfg.BloomExpectedInsert <= 0 {
		return fmt.Errorf("redis config: %s bloom expected insertions must be positive", name)
	}
	if cfg.BloomFalsePositive <= 0 || cfg.BloomFalsePositive >= 1 {
		return fmt.Errorf("redis config: %s bloom false positive rate must be in (0,1)", name)
	}
	return nil
}

func (c WarmupConfig) withDefaults() WarmupConfig {
	if c.Interval <= 0 {
		c.Interval = defaultWarmupInterval
	}
	if c.BatchSize <= 0 {
		c.BatchSize = defaultWarmupBatchSize
	}
	if c.Concurrency <= 0 {
		c.Concurrency = defaultWarmupConcurrency
	}
	if c.LockTTL <= 0 {
		c.LockTTL = defaultWarmupLockTTL
	}
	if c.StartupTopN <= 0 {
		c.StartupTopN = defaultWarmupStartupTopN
	}
	if c.HotsetRefresh <= 0 {
		c.HotsetRefresh = defaultHotsetRefresh
	}
	return c
}

func (c WarmupConfig) validate() error {
	cfg := c.withDefaults()
	if cfg.Interval <= 0 {
		return fmt.Errorf("redis config: warmup interval must be positive")
	}
	if cfg.BatchSize <= 0 {
		return fmt.Errorf("redis config: warmup batch size must be positive")
	}
	if cfg.Concurrency <= 0 {
		return fmt.Errorf("redis config: warmup concurrency must be positive")
	}
	if cfg.LockTTL <= 0 {
		return fmt.Errorf("redis config: warmup lock ttl must be positive")
	}
	if cfg.StartupTopN <= 0 {
		return fmt.Errorf("redis config: warmup startup top n must be positive")
	}
	if cfg.HotsetRefresh <= 0 {
		return fmt.Errorf("redis config: hotset refresh interval must be positive")
	}
	return nil
}
