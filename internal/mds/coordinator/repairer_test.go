package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"AstraStorage/internal/mds"
	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
	"AstraStorage/internal/platform/observability/metrics"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func TestPendingReplicaRepairer_RepairOncePromotesPendingReplica(t *testing.T) {
	repo := store.NewMemoryRepository()
	now := time.Now().UTC()

	if err := repo.CreateInode(context.Background(), &metadata.InodeMetadata{
		ID:        metadata.InodeID(metadata.RootInodeID),
		Path:      "/",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		LinkCount: 1,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create root inode: %v", err)
	}
	if err := repo.CreateInode(context.Background(), &metadata.InodeMetadata{
		ID:        "inode-1",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "demo.bin",
		Path:      "/demo.bin",
		Type:      metadata.InodeTypeFile,
		Status:    metadata.InodeStatusActive,
		FileID:    "file-1",
		LinkCount: 1,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create file inode: %v", err)
	}
	if err := repo.CreateFile(context.Background(), &metadata.FileMetadata{
		ID:            "file-1",
		InodeID:       "inode-1",
		ParentInodeID: metadata.InodeID(metadata.RootInodeID),
		Path:          "/demo.bin",
		Name:          "demo.bin",
		Size:          16,
		StoredSize:    16,
		ChunkSize:     metadata.FixedChunkSizeBytes,
		Status:        metadata.FileStatusAvailable,
		ReplicaPolicy: metadata.ReplicaPolicy{
			DesiredReplicaCount: metadata.DefaultReplicaCount,
			MinimumReplicaCount: metadata.MinimumReadableReplicaCount,
			CurrentReplicaCount: 2,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}

	checksum := metadata.Checksum{Algorithm: "sha256", Value: "abc", Verified: true, VerifiedAt: &now}
	if err := repo.UpsertChunks(context.Background(), []metadata.ChunkMetadata{{
		ID:       "chunk-1",
		FileID:   "file-1",
		Index:    0,
		Offset:   0,
		Size:     16,
		Status:   metadata.ChunkStatusPersisted,
		Checksum: checksum,
		Replicas: metadata.ReplicaSet{
			"node-1": {
				NodeID:     "node-1",
				FileID:     "file-1",
				ChunkID:    "chunk-1",
				Role:       metadata.ReplicaRolePrimary,
				State:      metadata.ReplicaStateReady,
				Checksum:   checksum,
				StoredSize: 16,
				CreatedAt:  now,
				UpdatedAt:  now,
				VerifiedAt: &now,
			},
			"node-2": {
				NodeID:    "node-2",
				FileID:    "file-1",
				ChunkID:   "chunk-1",
				Role:      metadata.ReplicaRoleSecondary,
				State:     metadata.ReplicaStatePending,
				Checksum:  checksum,
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		ReplicaCount: 2,
		CreatedAt:    now,
		UpdatedAt:    now,
	}}); err != nil {
		t.Fatalf("upsert chunk: %v", err)
	}
	if err := repo.UpsertNode(context.Background(), metadata.NodeInfo{ID: "node-1", Address: "http://node-1.local", Capacity: 1024, Used: 0, Healthy: true, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert source node: %v", err)
	}
	if err := repo.UpsertNode(context.Background(), metadata.NodeInfo{ID: "node-2", Address: "http://node-2.local", Capacity: 1024, Used: 0, Healthy: true, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert target node: %v", err)
	}

	repairer, err := newPendingReplicaRepairer(repo, PendingReplicaRepairerConfig{
		Interval:          time.Second,
		RetryBackoff:      time.Minute,
		MaxReplicasPerRun: 8,
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "http://node-1.local/internal/replicate" {
			t.Fatalf("unexpected repair request url: %s", req.URL.String())
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		if len(body) == 0 {
			t.Fatalf("expected repair request body")
		}
		payload, _ := json.Marshal(map[string]any{
			"replicas": []map[string]any{
				{"node_id": "node-2", "state": "ready", "address": "http://node-2.local"},
			},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})
	if err != nil {
		t.Fatalf("new repairer: %v", err)
	}

	if err := repairer.RepairOnce(context.Background()); err != nil {
		t.Fatalf("repair once: %v", err)
	}

	chunk, err := repo.GetChunk(context.Background(), store.ChunkSelector{ID: "chunk-1"})
	if err != nil {
		t.Fatalf("get repaired chunk: %v", err)
	}
	replica := chunk.Replicas["node-2"]
	if replica.State != metadata.ReplicaStateReady {
		t.Fatalf("expected repaired replica to be ready, got %#v", replica)
	}
	if replica.StoredSize != 16 {
		t.Fatalf("expected repaired replica size 16, got %d", replica.StoredSize)
	}
}

func TestPendingReplicaRepairer_RepairOnceRespectsMaxReplicasPerRun(t *testing.T) {
	repo := newRepairFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {
			NodeID:     "node-1",
			FileID:     "file-1",
			ChunkID:    "chunk-1",
			Role:       metadata.ReplicaRolePrimary,
			State:      metadata.ReplicaStateReady,
			Checksum:   metadata.Checksum{Algorithm: "sha256", Value: "abc"},
			StoredSize: 16,
		},
		"node-2": {
			NodeID:   "node-2",
			FileID:   "file-1",
			ChunkID:  "chunk-1",
			Role:     metadata.ReplicaRoleSecondary,
			State:    metadata.ReplicaStatePending,
			Checksum: metadata.Checksum{Algorithm: "sha256", Value: "abc"},
		},
		"node-3": {
			NodeID:   "node-3",
			FileID:   "file-1",
			ChunkID:  "chunk-1",
			Role:     metadata.ReplicaRoleSecondary,
			State:    metadata.ReplicaStatePending,
			Checksum: metadata.Checksum{Algorithm: "sha256", Value: "abc"},
		},
	}, "node-1", "node-2", "node-3")

	requestCount := 0
	repairer, err := newPendingReplicaRepairer(repo, PendingReplicaRepairerConfig{
		Interval:          time.Second,
		RetryBackoff:      time.Minute,
		MaxReplicasPerRun: 1,
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		payload, _ := json.Marshal(map[string]any{
			"replicas": []map[string]any{
				{"node_id": "node-2", "state": "ready", "address": "http://node-2.local"},
			},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})
	if err != nil {
		t.Fatalf("new repairer: %v", err)
	}

	if err := repairer.RepairOnce(context.Background()); err != nil {
		t.Fatalf("repair once: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected one repair request, got %d", requestCount)
	}

	chunk, err := repo.GetChunk(context.Background(), store.ChunkSelector{ID: "chunk-1"})
	if err != nil {
		t.Fatalf("get repaired chunk: %v", err)
	}
	if chunk.Replicas["node-2"].State != metadata.ReplicaStateReady {
		t.Fatalf("expected node-2 to be repaired, got %#v", chunk.Replicas["node-2"])
	}
	if chunk.Replicas["node-3"].State != metadata.ReplicaStatePending {
		t.Fatalf("expected node-3 to remain pending, got %#v", chunk.Replicas["node-3"])
	}
}

func TestPendingReplicaRepairer_RepairOnceBacksOffFailedReplica(t *testing.T) {
	repo := newRepairFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {
			NodeID:     "node-1",
			FileID:     "file-1",
			ChunkID:    "chunk-1",
			Role:       metadata.ReplicaRolePrimary,
			State:      metadata.ReplicaStateReady,
			Checksum:   metadata.Checksum{Algorithm: "sha256", Value: "abc"},
			StoredSize: 16,
		},
		"node-2": {
			NodeID:   "node-2",
			FileID:   "file-1",
			ChunkID:  "chunk-1",
			Role:     metadata.ReplicaRoleSecondary,
			State:    metadata.ReplicaStatePending,
			Checksum: metadata.Checksum{Algorithm: "sha256", Value: "abc"},
		},
	}, "node-1", "node-2")

	requestCount := 0
	repairer, err := newPendingReplicaRepairer(repo, PendingReplicaRepairerConfig{
		Interval:          time.Second,
		RetryBackoff:      time.Hour,
		MaxReplicasPerRun: 8,
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})
	if err != nil {
		t.Fatalf("new repairer: %v", err)
	}

	if err := repairer.RepairOnce(context.Background()); err == nil {
		t.Fatalf("expected first repair attempt to fail")
	}
	if err := repairer.RepairOnce(context.Background()); err != nil {
		t.Fatalf("expected second repair attempt to be skipped by backoff, got %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected failed replica to be retried once before backoff, got %d requests", requestCount)
	}
}

func TestPendingReplicaRepairer_RepairOnceSkipsFullTargets(t *testing.T) {
	repo := newRepairFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {
			NodeID:     "node-1",
			FileID:     "file-1",
			ChunkID:    "chunk-1",
			Role:       metadata.ReplicaRolePrimary,
			State:      metadata.ReplicaStateReady,
			Checksum:   metadata.Checksum{Algorithm: "sha256", Value: "abc"},
			StoredSize: 16,
		},
		"node-2": {
			NodeID:   "node-2",
			FileID:   "file-1",
			ChunkID:  "chunk-1",
			Role:     metadata.ReplicaRoleSecondary,
			State:    metadata.ReplicaStatePending,
			Checksum: metadata.Checksum{Algorithm: "sha256", Value: "abc"},
		},
	}, "node-1", "node-2")
	now := time.Now().UTC()
	if err := repo.UpsertNode(context.Background(), metadata.NodeInfo{
		ID:        "node-1",
		Address:   "http://node-1.local",
		Healthy:   true,
		Capacity:  100,
		Used:      10,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert node-1: %v", err)
	}
	if err := repo.UpsertNode(context.Background(), metadata.NodeInfo{
		ID:        "node-2",
		Address:   "http://node-2.local",
		Healthy:   true,
		Capacity:  100,
		Used:      100,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert node-2: %v", err)
	}

	requestCount := 0
	repairer, err := newPendingReplicaRepairer(repo, PendingReplicaRepairerConfig{
		Interval:          time.Second,
		RetryBackoff:      time.Minute,
		MaxReplicasPerRun: 8,
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})
	if err != nil {
		t.Fatalf("new repairer: %v", err)
	}

	if err := repairer.RepairOnce(context.Background()); err != nil {
		t.Fatalf("repair once: %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected full target to be skipped without replicate request, got %d", requestCount)
	}

	chunk, err := repo.GetChunk(context.Background(), store.ChunkSelector{ID: "chunk-1"})
	if err != nil {
		t.Fatalf("get chunk: %v", err)
	}
	if chunk.Replicas["node-2"].State != metadata.ReplicaStatePending {
		t.Fatalf("expected node-2 to remain pending, got %#v", chunk.Replicas["node-2"])
	}
}

func TestPendingReplicaRepairer_RepairOnceRepairsOnlyCapacityValidTargets(t *testing.T) {
	repo := newRepairFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {
			NodeID:     "node-1",
			FileID:     "file-1",
			ChunkID:    "chunk-1",
			Role:       metadata.ReplicaRolePrimary,
			State:      metadata.ReplicaStateReady,
			Checksum:   metadata.Checksum{Algorithm: "sha256", Value: "abc"},
			StoredSize: 16,
		},
		"node-2": {
			NodeID:   "node-2",
			FileID:   "file-1",
			ChunkID:  "chunk-1",
			Role:     metadata.ReplicaRoleSecondary,
			State:    metadata.ReplicaStatePending,
			Checksum: metadata.Checksum{Algorithm: "sha256", Value: "abc"},
		},
		"node-3": {
			NodeID:   "node-3",
			FileID:   "file-1",
			ChunkID:  "chunk-1",
			Role:     metadata.ReplicaRoleSecondary,
			State:    metadata.ReplicaStatePending,
			Checksum: metadata.Checksum{Algorithm: "sha256", Value: "abc"},
		},
	}, "node-1", "node-2", "node-3")
	now := time.Now().UTC()
	for _, node := range []metadata.NodeInfo{
		{ID: "node-1", Address: "http://node-1.local", Healthy: true, Capacity: 100, Used: 10, UpdatedAt: now},
		{ID: "node-2", Address: "http://node-2.local", Healthy: true, Capacity: 100, Used: 20, UpdatedAt: now},
		{ID: "node-3", Address: "http://node-3.local", Healthy: true, Capacity: 100, Used: 100, UpdatedAt: now},
	} {
		if err := repo.UpsertNode(context.Background(), node); err != nil {
			t.Fatalf("upsert node %q: %v", node.ID, err)
		}
	}

	requestCount := 0
	repairer, err := newPendingReplicaRepairer(repo, PendingReplicaRepairerConfig{
		Interval:          time.Second,
		RetryBackoff:      time.Minute,
		MaxReplicasPerRun: 8,
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		payload, _ := json.Marshal(map[string]any{
			"replicas": []map[string]any{
				{"node_id": "node-2", "state": "ready", "address": "http://node-2.local"},
			},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})
	if err != nil {
		t.Fatalf("new repairer: %v", err)
	}

	if err := repairer.RepairOnce(context.Background()); err != nil {
		t.Fatalf("repair once: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected one replicate request, got %d", requestCount)
	}

	chunk, err := repo.GetChunk(context.Background(), store.ChunkSelector{ID: "chunk-1"})
	if err != nil {
		t.Fatalf("get chunk: %v", err)
	}
	if chunk.Replicas["node-2"].State != metadata.ReplicaStateReady {
		t.Fatalf("expected node-2 to be repaired, got %#v", chunk.Replicas["node-2"])
	}
	if chunk.Replicas["node-3"].State != metadata.ReplicaStatePending {
		t.Fatalf("expected full node-3 to remain pending, got %#v", chunk.Replicas["node-3"])
	}
}

func TestPendingReplicaRepairer_RepairOnceRecordsMetricsAndRunID(t *testing.T) {
	repo := newRepairFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {
			NodeID:     "node-1",
			FileID:     "file-1",
			ChunkID:    "chunk-1",
			Role:       metadata.ReplicaRolePrimary,
			State:      metadata.ReplicaStateReady,
			Checksum:   metadata.Checksum{Algorithm: "sha256", Value: "abc"},
			StoredSize: 16,
		},
		"node-2": {
			NodeID:   "node-2",
			FileID:   "file-1",
			ChunkID:  "chunk-1",
			Role:     metadata.ReplicaRoleSecondary,
			State:    metadata.ReplicaStatePending,
			Checksum: metadata.Checksum{Algorithm: "sha256", Value: "abc"},
		},
		"node-3": {
			NodeID:   "node-3",
			FileID:   "file-1",
			ChunkID:  "chunk-1",
			Role:     metadata.ReplicaRoleSecondary,
			State:    metadata.ReplicaStatePending,
			Checksum: metadata.Checksum{Algorithm: "sha256", Value: "abc"},
		},
	}, "node-1", "node-2", "node-3")

	registry := metrics.NewRegistry("mds")
	obs, err := mds.NewObservability(registry)
	if err != nil {
		t.Fatalf("new observability: %v", err)
	}

	var logs bytes.Buffer
	previousFactory := repairerLoggerFactory
	repairerLoggerFactory = func() io.Writer { return &logs }
	defer func() {
		repairerLoggerFactory = previousFactory
	}()

	repairer, err := newPendingReplicaRepairer(repo, PendingReplicaRepairerConfig{
		Interval:          time.Second,
		RetryBackoff:      time.Minute,
		MaxReplicasPerRun: 1,
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		payload, _ := json.Marshal(map[string]any{
			"replicas": []map[string]any{
				{"node_id": "node-2", "state": "ready", "address": "http://node-2.local"},
			},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})
	if err != nil {
		t.Fatalf("new repairer: %v", err)
	}
	repairer.SetObservability(obs)

	if err := repairer.RepairOnce(context.Background()); err != nil {
		t.Fatalf("repair once: %v", err)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	registry.MetricsHandler().ServeHTTP(recorder, req)
	parser := expfmt.TextParser{}
	familiesMap, err := parser.TextToMetricFamilies(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatalf("parse metrics output: %v", err)
	}
	families := make([]*dto.MetricFamily, 0, len(familiesMap))
	for _, family := range familiesMap {
		families = append(families, family)
	}

	repairRuns := metricFamilyByName(t, families, "astrastorage_mds_repair_runs_total")
	assertMetricValue(t, repairRuns.GetMetric(), map[string]string{"result": "success"}, 1)

	repairDuration := metricFamilyByName(t, families, "astrastorage_mds_repair_run_duration_seconds")
	assertHistogramCount(t, repairDuration.GetMetric(), map[string]string{"result": "success"}, 1)

	attempted := metricFamilyByName(t, families, "astrastorage_mds_repair_replicas_attempted_total")
	assertMetricValue(t, attempted.GetMetric(), map[string]string{}, 1)

	succeeded := metricFamilyByName(t, families, "astrastorage_mds_repair_replicas_succeeded_total")
	assertMetricValue(t, succeeded.GetMetric(), map[string]string{}, 1)

	deferred := metricFamilyByName(t, families, "astrastorage_mds_repair_targets_deferred_total")
	assertMetricValue(t, deferred.GetMetric(), map[string]string{}, 0)

	if !bytes.Contains(logs.Bytes(), []byte(`"run_id"`)) {
		t.Fatalf("expected repair log to contain run_id, got %q", logs.String())
	}
}

func metricFamilyByName(t *testing.T, families []*dto.MetricFamily, name string) *dto.MetricFamily {
	t.Helper()
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}
	t.Fatalf("metric family %s not found", name)
	return nil
}

func assertMetricValue(t *testing.T, metrics []*dto.Metric, want map[string]string, value float64) {
	t.Helper()
	for _, metric := range metrics {
		matched := true
		for name, wantValue := range want {
			if labelValue(metric, name) != wantValue {
				matched = false
				break
			}
		}
		if matched {
			if got := counterValue(metric); got != value {
				t.Fatalf("expected metric value %v for labels %v, got %v", value, want, got)
			}
			return
		}
	}
	t.Fatalf("metric with labels %v not found", want)
}

func assertHistogramCount(t *testing.T, metrics []*dto.Metric, want map[string]string, count uint64) {
	t.Helper()
	for _, metric := range metrics {
		matched := true
		for name, value := range want {
			if labelValue(metric, name) != value {
				matched = false
				break
			}
		}
		if matched {
			if got := metric.GetHistogram().GetSampleCount(); got != count {
				t.Fatalf("expected histogram count %d for labels %v, got %d", count, want, got)
			}
			return
		}
	}
	t.Fatalf("histogram with labels %v not found", want)
}

func counterValue(metric *dto.Metric) float64 {
	if metric.GetCounter() != nil {
		return metric.GetCounter().GetValue()
	}
	return 0
}

func labelValue(metric *dto.Metric, name string) string {
	for _, label := range metric.GetLabel() {
		if label.GetName() == name {
			return label.GetValue()
		}
	}
	return ""
}

func newRepairFixtureRepository(t *testing.T, replicas metadata.ReplicaSet, nodeIDs ...metadata.NodeID) store.Repository {
	t.Helper()
	repo := store.NewMemoryRepository()
	now := time.Now().UTC()

	if err := repo.CreateInode(context.Background(), &metadata.InodeMetadata{
		ID:        metadata.InodeID(metadata.RootInodeID),
		Path:      "/",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		LinkCount: 1,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create root inode: %v", err)
	}
	if err := repo.CreateInode(context.Background(), &metadata.InodeMetadata{
		ID:        "inode-1",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "demo.bin",
		Path:      "/demo.bin",
		Type:      metadata.InodeTypeFile,
		Status:    metadata.InodeStatusActive,
		FileID:    "file-1",
		LinkCount: 1,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create file inode: %v", err)
	}
	if err := repo.CreateFile(context.Background(), &metadata.FileMetadata{
		ID:            "file-1",
		InodeID:       "inode-1",
		ParentInodeID: metadata.InodeID(metadata.RootInodeID),
		Path:          "/demo.bin",
		Name:          "demo.bin",
		Size:          16,
		StoredSize:    16,
		ChunkSize:     metadata.FixedChunkSizeBytes,
		Status:        metadata.FileStatusAvailable,
		ReplicaPolicy: metadata.ReplicaPolicy{
			DesiredReplicaCount: metadata.DefaultReplicaCount,
			MinimumReplicaCount: metadata.MinimumReadableReplicaCount,
			CurrentReplicaCount: len(replicas),
		},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}
	checksum := metadata.Checksum{Algorithm: "sha256", Value: "abc", Verified: true, VerifiedAt: &now}
	for nodeID, replica := range replicas {
		replica.NodeID = nodeID
		replica.FileID = "file-1"
		replica.ChunkID = "chunk-1"
		replica.Checksum = checksum
		replica.UpdatedAt = now
		if replica.CreatedAt.IsZero() {
			replica.CreatedAt = now
		}
		replicas[nodeID] = replica
	}
	if err := repo.UpsertChunks(context.Background(), []metadata.ChunkMetadata{{
		ID:           "chunk-1",
		FileID:       "file-1",
		Index:        0,
		Offset:       0,
		Size:         16,
		Status:       metadata.ChunkStatusPersisted,
		Checksum:     checksum,
		Replicas:     replicas,
		ReplicaCount: len(replicas),
		CreatedAt:    now,
		UpdatedAt:    now,
	}}); err != nil {
		t.Fatalf("upsert chunk: %v", err)
	}
	for _, nodeID := range nodeIDs {
		if err := repo.UpsertNode(context.Background(), metadata.NodeInfo{
			ID:        nodeID,
			Address:   "http://" + string(nodeID) + ".local",
			Capacity:  1024,
			Used:      0,
			Healthy:   true,
			UpdatedAt: now,
		}); err != nil {
			t.Fatalf("upsert node %s: %v", nodeID, err)
		}
	}
	return repo
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
