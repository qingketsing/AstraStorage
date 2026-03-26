package mds

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
	"AstraStorage/internal/platform/observability/metrics"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func TestObservability_ReusesCollectorsAcrossHandlers(t *testing.T) {
	registry := metrics.NewRegistry("mds")

	first, err := NewObservability(registry)
	if err != nil {
		t.Fatalf("new first observability: %v", err)
	}
	second, err := NewObservability(registry)
	if err != nil {
		t.Fatalf("new second observability: %v", err)
	}

	second.RecordRegisterNode("success")
	second.RecordHeartbeatNode("success")
	second.RecordStartUpload("success")
	second.RecordCommitChunk("success")
	second.RecordCompleteUpload("success")
	second.RecordBuildDownloadPlan("success")
	second.RecordAllocateUploadTargets("success")
	second.RecordRPCRequest("mds.start_upload", "success", 15*time.Millisecond)
	second.RecordRepairRun("success", 20*time.Millisecond)
	second.RecordRepairReplicasAttempted(2)
	second.RecordRepairReplicasSucceeded(1)
	second.RecordRepairReplicasFailed(1)
	second.RecordRepairTargetsDeferred(3)
	second.RecordLeaderTransition("started")
	second.RecordLeaderTransition("stopped")
	second.SetLeaderState(false, 0)
	second.RecordLeaderElectionFailure()

	_ = first

	families := scrapeMDSMetricFamilies(t, registry)

	registers := metricFamilyByName(t, families, "astrastorage_mds_nodes_registered_total")
	assertMetricValue(t, registers.GetMetric(), map[string]string{"result": "success"}, 1)

	heartbeats := metricFamilyByName(t, families, "astrastorage_mds_node_heartbeats_total")
	assertMetricValue(t, heartbeats.GetMetric(), map[string]string{"result": "success"}, 1)

	starts := metricFamilyByName(t, families, "astrastorage_mds_upload_sessions_started_total")
	assertMetricValue(t, starts.GetMetric(), map[string]string{"result": "success"}, 1)

	commits := metricFamilyByName(t, families, "astrastorage_mds_chunks_committed_total")
	assertMetricValue(t, commits.GetMetric(), map[string]string{"result": "success"}, 1)

	completes := metricFamilyByName(t, families, "astrastorage_mds_uploads_completed_total")
	assertMetricValue(t, completes.GetMetric(), map[string]string{"result": "success"}, 1)

	plans := metricFamilyByName(t, families, "astrastorage_mds_download_plans_built_total")
	assertMetricValue(t, plans.GetMetric(), map[string]string{"result": "success"}, 1)

	allocations := metricFamilyByName(t, families, "astrastorage_mds_allocate_upload_targets_total")
	assertMetricValue(t, allocations.GetMetric(), map[string]string{"result": "success"}, 1)

	rpcRequests := metricFamilyByName(t, families, "astrastorage_mds_rpc_requests_total")
	assertMetricValue(t, rpcRequests.GetMetric(), map[string]string{
		"method": "mds.start_upload",
		"result": "success",
	}, 1)

	rpcDuration := metricFamilyByName(t, families, "astrastorage_mds_rpc_request_duration_seconds")
	assertHistogramCount(t, rpcDuration.GetMetric(), map[string]string{
		"method": "mds.start_upload",
		"result": "success",
	}, 1)

	repairRuns := metricFamilyByName(t, families, "astrastorage_mds_repair_runs_total")
	assertMetricValue(t, repairRuns.GetMetric(), map[string]string{"result": "success"}, 1)

	repairDuration := metricFamilyByName(t, families, "astrastorage_mds_repair_run_duration_seconds")
	assertHistogramCount(t, repairDuration.GetMetric(), map[string]string{"result": "success"}, 1)

	attempted := metricFamilyByName(t, families, "astrastorage_mds_repair_replicas_attempted_total")
	assertMetricValue(t, attempted.GetMetric(), map[string]string{}, 2)

	succeeded := metricFamilyByName(t, families, "astrastorage_mds_repair_replicas_succeeded_total")
	assertMetricValue(t, succeeded.GetMetric(), map[string]string{}, 1)

	failed := metricFamilyByName(t, families, "astrastorage_mds_repair_replicas_failed_total")
	assertMetricValue(t, failed.GetMetric(), map[string]string{}, 1)

	deferred := metricFamilyByName(t, families, "astrastorage_mds_repair_targets_deferred_total")
	assertMetricValue(t, deferred.GetMetric(), map[string]string{}, 3)

	leaderTransitions := metricFamilyByName(t, families, "astrastorage_mds_leader_transitions_total")
	assertMetricValue(t, leaderTransitions.GetMetric(), map[string]string{"result": "started"}, 1)
	assertMetricValue(t, leaderTransitions.GetMetric(), map[string]string{"result": "stopped"}, 1)

	leaderIsLeader := metricFamilyByName(t, families, "astrastorage_mds_leader_is_leader")
	assertMetricValue(t, leaderIsLeader.GetMetric(), map[string]string{}, 0)

	leaderTerm := metricFamilyByName(t, families, "astrastorage_mds_leader_term")
	assertMetricValue(t, leaderTerm.GetMetric(), map[string]string{}, 0)

	leaderFailures := metricFamilyByName(t, families, "astrastorage_mds_leader_election_failures_total")
	assertMetricValue(t, leaderFailures.GetMetric(), map[string]string{}, 1)
}

func TestHandler_RecordsBusinessMetrics(t *testing.T) {
	registry := metrics.NewRegistry("mds")
	obs, err := NewObservability(registry)
	if err != nil {
		t.Fatalf("new observability: %v", err)
	}

	repo := store.NewMemoryRepository()
	service, err := NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := NewHandler(service)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	handler.SetObservability(obs)

	now := time.Now().UTC()
	mustCreateRootInode(t, repo, now)

	if _, err := handler.RegisterNode(context.Background(), RegisterNodeRequest{
		ID:        "node-1",
		Address:   "http://127.0.0.1:28080",
		Capacity:  2048,
		Used:      128,
		Healthy:   true,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("register node: %v", err)
	}
	if _, err := handler.HeartbeatNode(context.Background(), HeartbeatNodeRequest{
		NodeID:     "node-1",
		Healthy:    true,
		Capacity:   2048,
		Used:       256,
		LastSeenAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("heartbeat node: %v", err)
	}
	if _, err := handler.CreateFile(context.Background(), CreateFileRequest{
		InodeID:   "file-inode-1",
		FileID:    "file-1",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "demo.bin",
		Size:      16,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if _, err := handler.AllocateUploadTargets(context.Background(), AllocateUploadTargetsRequest{
		FileID:     "file-1",
		ChunkIndex: 0,
	}); err != nil {
		t.Fatalf("allocate upload targets: %v", err)
	}
	if _, err := handler.StartUpload(context.Background(), StartUploadRequest{
		SessionID:    "session-1",
		FileID:       "file-1",
		ExpectedSize: 16,
		CreatedAt:    now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("start upload: %v", err)
	}
	if _, err := handler.CommitChunk(context.Background(), CommitChunkRequest{
		SessionID: "session-1",
		ChunkID:   "chunk-1",
		Index:     0,
		Offset:    0,
		Size:      16,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "chunk-1",
			Verified:   true,
			VerifiedAt: timePtr(now.Add(3 * time.Minute)),
		},
		Replicas: metadata.ReplicaSet{
			"node-1": {
				NodeID: "node-1",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("commit chunk: %v", err)
	}
	if _, err := handler.CompleteUpload(context.Background(), CompleteUploadRequest{
		SessionID:        "session-1",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusUploading},
		CompletedAt:      now.Add(4 * time.Minute),
	}); err != nil {
		t.Fatalf("complete upload: %v", err)
	}
	if _, err := handler.BuildDownloadPlan(context.Background(), "file-1"); err != nil {
		t.Fatalf("build download plan: %v", err)
	}

	families := scrapeMDSMetricFamilies(t, registry)

	registers := metricFamilyByName(t, families, "astrastorage_mds_nodes_registered_total")
	assertMetricValue(t, registers.GetMetric(), map[string]string{"result": "success"}, 1)

	heartbeats := metricFamilyByName(t, families, "astrastorage_mds_node_heartbeats_total")
	assertMetricValue(t, heartbeats.GetMetric(), map[string]string{"result": "success"}, 1)

	allocations := metricFamilyByName(t, families, "astrastorage_mds_allocate_upload_targets_total")
	assertMetricValue(t, allocations.GetMetric(), map[string]string{"result": "success"}, 1)

	starts := metricFamilyByName(t, families, "astrastorage_mds_upload_sessions_started_total")
	assertMetricValue(t, starts.GetMetric(), map[string]string{"result": "success"}, 1)

	commits := metricFamilyByName(t, families, "astrastorage_mds_chunks_committed_total")
	assertMetricValue(t, commits.GetMetric(), map[string]string{"result": "success"}, 1)

	completes := metricFamilyByName(t, families, "astrastorage_mds_uploads_completed_total")
	assertMetricValue(t, completes.GetMetric(), map[string]string{"result": "success"}, 1)

	plans := metricFamilyByName(t, families, "astrastorage_mds_download_plans_built_total")
	assertMetricValue(t, plans.GetMetric(), map[string]string{"result": "success"}, 1)
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func mustCreateRootInode(t *testing.T, repo store.Repository, now time.Time) {
	t.Helper()
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
}

func scrapeMDSMetricFamilies(t *testing.T, registry *metrics.Registry) []*dto.MetricFamily {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	registry.MetricsHandler().ServeHTTP(recorder, req)
	parser := expfmt.TextParser{}
	familiesMap, err := parser.TextToMetricFamilies(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatalf("parse metrics output: %v", err)
	}
	result := make([]*dto.MetricFamily, 0, len(familiesMap))
	for _, family := range familiesMap {
		result = append(result, family)
	}
	return result
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
	if metric.GetGauge() != nil {
		return metric.GetGauge().GetValue()
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
