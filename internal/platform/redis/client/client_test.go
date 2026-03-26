package client

import "testing"

func TestNewBundle_CreatesCacheAndCoordGroups(t *testing.T) {
	cfg := Config{
		Enabled:           true,
		SentinelEndpoints: []string{"127.0.0.1:26379", "127.0.0.1:26380"},
		Cache: ReplicationGroupConfig{
			MasterSetName: "astra-cache",
		},
		Coord: ReplicationGroupConfig{
			MasterSetName: "astra-coord",
		},
	}

	bundle, err := NewBundle(cfg)
	if err != nil {
		t.Fatalf("new bundle: %v", err)
	}
	defer func() {
		if err := bundle.Close(); err != nil {
			t.Fatalf("close bundle: %v", err)
		}
	}()

	if bundle.Cache() == nil {
		t.Fatal("expected cache group clients")
	}
	if bundle.Coord() == nil {
		t.Fatal("expected coord group clients")
	}
	if bundle.Cache().Group() != GroupCache {
		t.Fatalf("expected cache group, got %q", bundle.Cache().Group())
	}
	if bundle.Coord().Group() != GroupCoord {
		t.Fatalf("expected coord group, got %q", bundle.Coord().Group())
	}
}

func TestNewBundle_RequiresRedisToBeEnabled(t *testing.T) {
	if _, err := NewBundle(Config{}); err == nil {
		t.Fatalf("expected disabled redis config to fail")
	}
}

func TestGroupClients_UsesReplicaOnlyReadClient(t *testing.T) {
	cfg := Config{
		Enabled:           true,
		SentinelEndpoints: []string{"127.0.0.1:26379"},
		Cache: ReplicationGroupConfig{
			MasterSetName: "astra-cache",
		},
		Coord: ReplicationGroupConfig{
			MasterSetName: "astra-coord",
		},
	}

	bundle, err := NewBundle(cfg)
	if err != nil {
		t.Fatalf("new bundle: %v", err)
	}
	defer bundle.Close()

	writeOpts := bundle.Cache().WriteOptions()
	readOpts := bundle.Cache().ReadOptions()
	if writeOpts.ReplicaOnly {
		t.Fatalf("expected write client to target master")
	}
	if !readOpts.ReplicaOnly {
		t.Fatalf("expected read client to target replicas")
	}
	if readOpts.RouteRandomly {
		t.Fatalf("expected read client to avoid unsupported random routing mode")
	}
	if readOpts.MasterName != "astra-cache" {
		t.Fatalf("unexpected read master name %q", readOpts.MasterName)
	}
}

func TestBundleHealthSummaries_ReportSentinelTopology(t *testing.T) {
	cfg := Config{
		Enabled:           true,
		SentinelEndpoints: []string{"127.0.0.1:26379", "127.0.0.1:26380", "127.0.0.1:26381"},
		Cache: ReplicationGroupConfig{
			MasterSetName: "astra-cache",
		},
		Coord: ReplicationGroupConfig{
			MasterSetName: "astra-coord",
		},
	}

	bundle, err := NewBundle(cfg)
	if err != nil {
		t.Fatalf("new bundle: %v", err)
	}
	defer bundle.Close()

	summaries := bundle.HealthSummaries()
	if len(summaries) != 2 {
		t.Fatalf("expected 2 health summaries, got %d", len(summaries))
	}
	if summaries[0].MasterSetName == summaries[1].MasterSetName {
		t.Fatalf("expected distinct master sets in health summaries")
	}
	if len(summaries[0].SentinelEndpoints) != 3 {
		t.Fatalf("expected sentinel endpoints to be preserved, got %#v", summaries[0].SentinelEndpoints)
	}
}
