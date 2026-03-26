package client

import (
	"testing"
	"time"
)

func TestConfigWithDefaults_SetsRedisSentinelDefaults(t *testing.T) {
	cfg := Config{
		SentinelEndpoints: []string{"127.0.0.1:26379"},
		Cache: ReplicationGroupConfig{
			MasterSetName: "astra-cache",
		},
		Coord: ReplicationGroupConfig{
			MasterSetName: "astra-coord",
		},
	}

	cfg = cfg.WithDefaults()

	if cfg.DialTimeout <= 0 {
		t.Fatalf("expected positive dial timeout, got %s", cfg.DialTimeout)
	}
	if cfg.ReadTimeout <= 0 {
		t.Fatalf("expected positive read timeout, got %s", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout <= 0 {
		t.Fatalf("expected positive write timeout, got %s", cfg.WriteTimeout)
	}
	if cfg.Cache.FileMetaTTL <= 0 {
		t.Fatalf("expected positive file meta ttl, got %s", cfg.Cache.FileMetaTTL)
	}
	if cfg.Cache.DownloadPlanTTL <= 0 {
		t.Fatalf("expected positive download plan ttl, got %s", cfg.Cache.DownloadPlanTTL)
	}
	if cfg.Cache.NullEntryTTL <= 0 {
		t.Fatalf("expected positive null entry ttl, got %s", cfg.Cache.NullEntryTTL)
	}
	if cfg.Warmup.Interval <= 0 {
		t.Fatalf("expected positive warmup interval, got %s", cfg.Warmup.Interval)
	}
}

func TestConfigValidate_RequiresSentinelEndpointsAndMasterSets(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Cache: ReplicationGroupConfig{
			MasterSetName: "astra-cache",
		},
		Coord: ReplicationGroupConfig{
			MasterSetName: "astra-coord",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected missing sentinel endpoints to fail validation")
	}

	cfg.SentinelEndpoints = []string{"127.0.0.1:26379"}
	cfg.Cache.MasterSetName = ""
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected missing cache master set to fail validation")
	}

	cfg.Cache.MasterSetName = "astra-cache"
	cfg.Coord.MasterSetName = ""
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected missing coord master set to fail validation")
	}
}

func TestConfigValidate_RejectsSharedMasterSetNames(t *testing.T) {
	cfg := Config{
		Enabled:           true,
		SentinelEndpoints: []string{"127.0.0.1:26379"},
		Cache: ReplicationGroupConfig{
			MasterSetName: "astra-shared",
		},
		Coord: ReplicationGroupConfig{
			MasterSetName: "astra-shared",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected shared master set names to fail validation")
	}
}

func TestConfigWithDefaults_PreservesExplicitValues(t *testing.T) {
	cfg := Config{
		SentinelEndpoints: []string{"127.0.0.1:26379"},
		DialTimeout:       2 * time.Second,
		ReadTimeout:       3 * time.Second,
		WriteTimeout:      4 * time.Second,
		Cache: ReplicationGroupConfig{
			MasterSetName:       "astra-cache",
			FileMetaTTL:         5 * time.Minute,
			FileMetaTTLJitter:   45 * time.Second,
			DownloadPlanTTL:     2 * time.Minute,
			DirectoryListTTL:    90 * time.Second,
			NodeHealthTTL:       15 * time.Second,
			NullEntryTTL:        30 * time.Second,
			HotspotThreshold:    12,
			HotspotWindow:       time.Minute,
			StaleServeWindow:    20 * time.Second,
			BloomExpectedInsert: 1000,
			BloomFalsePositive:  0.01,
		},
		Coord: ReplicationGroupConfig{
			MasterSetName: "astra-coord",
		},
		Warmup: WarmupConfig{
			Interval:      25 * time.Second,
			BatchSize:     10,
			Concurrency:   4,
			LockTTL:       8 * time.Second,
			StartupTopN:   100,
			HotsetRefresh: 2 * time.Minute,
		},
	}

	got := cfg.WithDefaults()
	if got.DialTimeout != 2*time.Second {
		t.Fatalf("expected dial timeout to be preserved, got %s", got.DialTimeout)
	}
	if got.Cache.FileMetaTTLJitter != 45*time.Second {
		t.Fatalf("expected file meta ttl jitter to be preserved, got %s", got.Cache.FileMetaTTLJitter)
	}
	if got.Warmup.Interval != 25*time.Second {
		t.Fatalf("expected warmup interval to be preserved, got %s", got.Warmup.Interval)
	}
}
