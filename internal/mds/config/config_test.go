package config

import (
	"os"
	"testing"
	"time"
)

func TestConfigValidate_LeaderElectionRequiresEndpoints(t *testing.T) {
	cfg := Config{
		Backend: BackendMemory,
		HTTP:    HTTPConfig{}.WithDefaults(),
		Repair:  RepairConfig{}.WithDefaults(),
		LeaderElection: LeaderElectionConfig{
			Enabled: true,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected leader election config without etcd endpoints to fail")
	}
}

func TestLoadFromEnv_LoadsLeaderElectionConfig(t *testing.T) {
	t.Setenv("MDS_LEADER_ELECTION_ENABLED", "true")
	t.Setenv("MDS_ETCD_ENDPOINTS", " http://127.0.0.1:2379 , http://127.0.0.1:22379 ")
	t.Setenv("MDS_ETCD_DIAL_TIMEOUT", "3s")
	t.Setenv("MDS_LEADER_ELECTION_PREFIX", "/astra/mds/leader")
	t.Setenv("MDS_LEADER_LEASE_TTL", "9s")
	t.Setenv("MDS_INSTANCE_ID", "mds-test-1")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("load config from env: %v", err)
	}

	if !cfg.LeaderElection.Enabled {
		t.Fatalf("expected leader election to be enabled")
	}
	if len(cfg.LeaderElection.EtcdEndpoints) != 2 {
		t.Fatalf("expected 2 etcd endpoints, got %#v", cfg.LeaderElection.EtcdEndpoints)
	}
	if cfg.LeaderElection.EtcdEndpoints[0] != "http://127.0.0.1:2379" {
		t.Fatalf("unexpected first etcd endpoint %q", cfg.LeaderElection.EtcdEndpoints[0])
	}
	if cfg.LeaderElection.DialTimeout != 3*time.Second {
		t.Fatalf("expected dial timeout 3s, got %s", cfg.LeaderElection.DialTimeout)
	}
	if cfg.LeaderElection.Prefix != "/astra/mds/leader" {
		t.Fatalf("unexpected leader election prefix %q", cfg.LeaderElection.Prefix)
	}
	if cfg.LeaderElection.LeaseTTL != 9*time.Second {
		t.Fatalf("unexpected leader lease ttl %s", cfg.LeaderElection.LeaseTTL)
	}
	if cfg.LeaderElection.InstanceID != "mds-test-1" {
		t.Fatalf("unexpected instance id %q", cfg.LeaderElection.InstanceID)
	}
}

func TestLoadFromEnv_LeaderElectionDisabledAllowsEmptyEndpoints(t *testing.T) {
	for _, name := range []string{
		"MDS_LEADER_ELECTION_ENABLED",
		"MDS_ETCD_ENDPOINTS",
		"MDS_ETCD_DIAL_TIMEOUT",
		"MDS_LEADER_ELECTION_PREFIX",
		"MDS_LEADER_LEASE_TTL",
		"MDS_INSTANCE_ID",
	} {
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("load config from env: %v", err)
	}
	if cfg.LeaderElection.Enabled {
		t.Fatalf("expected leader election to be disabled by default")
	}
	if len(cfg.LeaderElection.EtcdEndpoints) != 0 {
		t.Fatalf("expected no etcd endpoints when disabled, got %#v", cfg.LeaderElection.EtcdEndpoints)
	}
}

func TestConfigValidate_RedisRequiresSentinelEndpointsWhenEnabled(t *testing.T) {
	cfg := Config{
		Backend:   BackendMemory,
		HTTP:      HTTPConfig{}.WithDefaults(),
		Repair:    RepairConfig{}.WithDefaults(),
		Failover:  FailoverConfig{}.WithDefaults(),
		Cleanup:   CleanupConfig{}.WithDefaults(),
		Rebalance: RebalanceConfig{}.WithDefaults(),
		Redis: RedisConfig{
			Enabled: true,
			Cache: RedisReplicationGroupConfig{
				MasterSetName: "astra-cache",
			},
			Coord: RedisReplicationGroupConfig{
				MasterSetName: "astra-coord",
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected redis config without sentinel endpoints to fail")
	}
}

func TestLoadFromEnv_LoadsRedisConfig(t *testing.T) {
	t.Setenv("MDS_REDIS_ENABLED", "true")
	t.Setenv("MDS_REDIS_SENTINEL_ENDPOINTS", " 127.0.0.1:26379,127.0.0.1:26380 , 127.0.0.1:26381 ")
	t.Setenv("MDS_REDIS_CACHE_MASTER_SET", "astra-cache")
	t.Setenv("MDS_REDIS_COORD_MASTER_SET", "astra-coord")
	t.Setenv("MDS_REDIS_DIAL_TIMEOUT", "2s")
	t.Setenv("MDS_REDIS_READ_TIMEOUT", "750ms")
	t.Setenv("MDS_REDIS_WRITE_TIMEOUT", "1s")
	t.Setenv("MDS_REDIS_FILE_META_TTL", "5m")
	t.Setenv("MDS_REDIS_FILE_META_TTL_JITTER", "45s")
	t.Setenv("MDS_REDIS_DOWNLOAD_PLAN_TTL", "3m")
	t.Setenv("MDS_REDIS_NULL_ENTRY_TTL", "30s")
	t.Setenv("MDS_REDIS_WARMUP_INTERVAL", "20s")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("load config from env: %v", err)
	}

	if !cfg.Redis.Enabled {
		t.Fatalf("expected redis to be enabled")
	}
	if len(cfg.Redis.SentinelEndpoints) != 3 {
		t.Fatalf("expected 3 sentinel endpoints, got %#v", cfg.Redis.SentinelEndpoints)
	}
	if cfg.Redis.SentinelEndpoints[0] != "127.0.0.1:26379" {
		t.Fatalf("unexpected first sentinel endpoint %q", cfg.Redis.SentinelEndpoints[0])
	}
	if cfg.Redis.Cache.MasterSetName != "astra-cache" {
		t.Fatalf("unexpected cache master set %q", cfg.Redis.Cache.MasterSetName)
	}
	if cfg.Redis.Coord.MasterSetName != "astra-coord" {
		t.Fatalf("unexpected coord master set %q", cfg.Redis.Coord.MasterSetName)
	}
	if cfg.Redis.DialTimeout != 2*time.Second {
		t.Fatalf("unexpected redis dial timeout %s", cfg.Redis.DialTimeout)
	}
	if cfg.Redis.ReadTimeout != 750*time.Millisecond {
		t.Fatalf("unexpected redis read timeout %s", cfg.Redis.ReadTimeout)
	}
	if cfg.Redis.WriteTimeout != time.Second {
		t.Fatalf("unexpected redis write timeout %s", cfg.Redis.WriteTimeout)
	}
	if cfg.Redis.Cache.FileMetaTTL != 5*time.Minute {
		t.Fatalf("unexpected file meta ttl %s", cfg.Redis.Cache.FileMetaTTL)
	}
	if cfg.Redis.Cache.FileMetaTTLJitter != 45*time.Second {
		t.Fatalf("unexpected file meta ttl jitter %s", cfg.Redis.Cache.FileMetaTTLJitter)
	}
	if cfg.Redis.Cache.DownloadPlanTTL != 3*time.Minute {
		t.Fatalf("unexpected download plan ttl %s", cfg.Redis.Cache.DownloadPlanTTL)
	}
	if cfg.Redis.Cache.NullEntryTTL != 30*time.Second {
		t.Fatalf("unexpected null entry ttl %s", cfg.Redis.Cache.NullEntryTTL)
	}
	if cfg.Redis.Warmup.Interval != 20*time.Second {
		t.Fatalf("unexpected warmup interval %s", cfg.Redis.Warmup.Interval)
	}
}
