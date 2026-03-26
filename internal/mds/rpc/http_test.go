package rpc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"AstraStorage/internal/mds"
	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/rpc"
	"AstraStorage/internal/mds/store"
	"AstraStorage/internal/platform/observability/metrics"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func TestHTTPHandler_UploadLifecycle(t *testing.T) {
	repo := store.NewMemoryRepository()
	service, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := mds.NewHandler(service)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	router, err := rpc.NewRouter(handler)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	httpHandler, err := rpc.NewHTTPHandler(router, repo, metrics.NewRegistry("mds"))
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	ctx := context.Background()
	now := time.Now().UTC()
	chunkVerifiedAt := now.Add(90 * time.Second)
	fileVerifiedAt := now.Add(150 * time.Second)
	mustCreateRoot(t, ctx, repo, now)

	postRPCAndDecode(t, httpHandler, rpc.MethodCreateFile, rpc.CreateFileRequest{
		InodeID:   "http-file-inode",
		FileID:    "http-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "http.txt",
		Size:      64,
		CreatedAt: now,
	}, &rpc.CreateFileResponse{})

	postRPCAndDecode(t, httpHandler, rpc.MethodStartUpload, rpc.StartUploadRequest{
		SessionID:    "http-session",
		FileID:       "http-file",
		ExpectedSize: 64,
		CreatedAt:    now.Add(time.Minute),
	}, &rpc.StartUploadResponse{})

	postRPCAndDecode(t, httpHandler, rpc.MethodCommitChunk, rpc.CommitChunkRequest{
		SessionID: "http-session",
		ChunkID:   "http-chunk-0",
		Index:     0,
		Offset:    0,
		Size:      64,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "http-chunk-0",
			Verified:   true,
			VerifiedAt: &chunkVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-1": {
				NodeID: "node-1",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(2 * time.Minute),
	}, &rpc.CommitChunkResponse{})

	completeResp := &rpc.CompleteUploadResponse{}
	postRPCAndDecode(t, httpHandler, rpc.MethodCompleteUpload, rpc.CompleteUploadRequest{
		SessionID:        "http-session",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusUploading},
		CompletedAt:      now.Add(3 * time.Minute),
	}, completeResp)
	if completeResp.File == nil || completeResp.File.Status != metadata.FileStatusVerifying {
		t.Fatalf("expected verifying file after complete, got %#v", completeResp.File)
	}

	verifyResp := &rpc.VerifyUploadResponse{}
	postRPCAndDecode(t, httpHandler, rpc.MethodVerifyUpload, rpc.VerifyUploadRequest{
		SessionID: "http-session",
		VerifiedChecksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "http-file",
			Verified:   true,
			VerifiedAt: &fileVerifiedAt,
		},
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusVerifying},
		VerifiedAt:       now.Add(4 * time.Minute),
	}, verifyResp)
	if verifyResp.File == nil || verifyResp.File.Status != metadata.FileStatusAvailable {
		t.Fatalf("expected available file after verify, got %#v", verifyResp.File)
	}

	planResp := &rpc.BuildDownloadPlanResponse{}
	postRPCAndDecode(t, httpHandler, rpc.MethodBuildDownloadPlan, rpc.BuildDownloadPlanRequest{
		FileID: "http-file",
	}, planResp)
	if planResp.Plan == nil || planResp.Plan.ChunkCount != 1 {
		t.Fatalf("expected download plan with 1 chunk, got %#v", planResp.Plan)
	}
	if planResp.Plan.Chunks[0].PreferredNodeID != "node-1" {
		t.Fatalf("expected preferred node node-1, got %#v", planResp.Plan.Chunks[0])
	}
}

func TestHTTPHandler_RecordsNormalizedRouteMetrics(t *testing.T) {
	httpHandler, registry := newMDSHTTPHandler(t)

	for _, method := range []string{rpc.MethodCreateFile, rpc.MethodStartUpload} {
		req := httptest.NewRequest(http.MethodPost, "/rpc/"+method, bytes.NewReader([]byte(`{"bad":`)))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		httpHandler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 from rpc route %s, got %d", method, recorder.Code)
		}
	}

	unmatchedReq := httptest.NewRequest(http.MethodGet, "/rpc-bogus", nil)
	unmatchedRecorder := httptest.NewRecorder()
	httpHandler.ServeHTTP(unmatchedRecorder, unmatchedReq)
	if unmatchedRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 from unmatched route, got %d", unmatchedRecorder.Code)
	}

	for _, path := range []string{"/rpc/", "/rpc/a/b"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()
		httpHandler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("expected 404 from malformed route %s, got %d", path, recorder.Code)
		}
	}

	families := scrapeMetricsFamilies(t, registry)
	requests := metricFamilyByName(t, families, "astrastorage_http_requests_total")
	assertMetricLabels(t, requests.GetMetric(), map[string]string{"service": "mds", "route": "/rpc/:method", "status_class": "4xx"})
	assertMetricValue(t, requests.GetMetric(), map[string]string{"service": "mds", "route": "/rpc/:method", "status_class": "4xx"}, 2)
	assertMetricLabels(t, requests.GetMetric(), map[string]string{"service": "mds", "route": "/unmatched", "status_class": "4xx"})
	assertMetricValue(t, requests.GetMetric(), map[string]string{"service": "mds", "route": "/unmatched", "status_class": "4xx"}, 3)
}

func TestHTTPHandler_RecordsRPCMethodMetrics(t *testing.T) {
	httpHandler, registry := newMDSHTTPHandler(t)

	body := bytes.NewReader([]byte(`{"bad":`))
	req := httptest.NewRequest(http.MethodPost, "/rpc/"+rpc.MethodCreateFile, body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	httpHandler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 from malformed create file request, got %d", recorder.Code)
	}

	families := scrapeMetricsFamilies(t, registry)
	rpcRequests := metricFamilyByName(t, families, "astrastorage_mds_rpc_requests_total")
	assertMetricValue(t, rpcRequests.GetMetric(), map[string]string{
		"method": rpc.MethodCreateFile,
		"result": "invalid_argument",
	}, 1)

	rpcDuration := metricFamilyByName(t, families, "astrastorage_mds_rpc_request_duration_seconds")
	assertHistogramCount(t, rpcDuration.GetMetric(), map[string]string{
		"method": rpc.MethodCreateFile,
		"result": "invalid_argument",
	}, 1)
}

func TestHTTPHandler_HealthAndErrorMapping(t *testing.T) {
	repo := store.NewMemoryRepository()
	service, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := mds.NewHandler(service)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	router, err := rpc.NewRouter(handler)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	httpHandler, err := rpc.NewHTTPHandler(router, repo, metrics.NewRegistry("mds"))
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	now := time.Now().UTC()
	mustCreateRoot(t, context.Background(), repo, now)

	resp := performRequest(t, httpHandler, http.MethodGet, "/healthz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from healthz, got %d", resp.StatusCode)
	}

	postRPCAndDecode(t, httpHandler, rpc.MethodCreateFile, rpc.CreateFileRequest{
		InodeID:   "dup-inode",
		FileID:    "dup-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "dup.txt",
		Size:      32,
		CreatedAt: now,
	}, &rpc.CreateFileResponse{})

	body, err := json.Marshal(rpc.CreateFileRequest{
		InodeID:   "dup-inode-2",
		FileID:    "dup-file-2",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "dup.txt",
		Size:      32,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("marshal duplicate request: %v", err)
	}
	httpResp := performRequest(t, httpHandler, http.MethodPost, "/rpc/"+rpc.MethodCreateFile, bytes.NewReader(body))
	if httpResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate file, got %d", httpResp.StatusCode)
	}

	httpResp = performRequest(t, httpHandler, http.MethodPost, "/rpc/mds.unknown", bytes.NewReader([]byte(`{}`)))
	if httpResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown method, got %d", httpResp.StatusCode)
	}
}

func newMDSHTTPHandler(t *testing.T) (http.Handler, *metrics.Registry) {
	t.Helper()
	repo := store.NewMemoryRepository()
	service, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := mds.NewHandler(service)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	router, err := rpc.NewRouter(handler)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	registry := metrics.NewRegistry("mds")
	httpHandler, err := rpc.NewHTTPHandler(router, repo, registry)
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}
	return httpHandler, registry
}

func scrapeMetricsFamilies(t *testing.T, registry *metrics.Registry) []*dto.MetricFamily {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	registry.MetricsHandler().ServeHTTP(recorder, req)
	parser := expfmt.TextParser{}
	families, err := parser.TextToMetricFamilies(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatalf("parse metrics output: %v", err)
	}
	result := make([]*dto.MetricFamily, 0, len(families))
	for _, family := range families {
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
			if got := metric.GetCounter().GetValue(); got != value {
				t.Fatalf("expected metric value %v for labels %v, got %v", value, want, got)
			}
			return
		}
	}
	t.Fatalf("metric with labels %v not found", want)
}

func assertMetricLabels(t *testing.T, metrics []*dto.Metric, want map[string]string) {
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

func labelValue(metric *dto.Metric, name string) string {
	for _, label := range metric.GetLabel() {
		if label.GetName() == name {
			return label.GetValue()
		}
	}
	return ""
}

func TestHTTPHandler_RegisterNode(t *testing.T) {
	repo := store.NewMemoryRepository()
	service, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := mds.NewHandler(service)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	router, err := rpc.NewRouter(handler)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	httpHandler, err := rpc.NewHTTPHandler(router, repo, metrics.NewRegistry("mds"))
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	now := time.Now().UTC()
	resp := &rpc.RegisterNodeResponse{}
	postRPCAndDecode(t, httpHandler, rpc.MethodRegisterNode, rpc.RegisterNodeRequest{
		ID:        "http-node-1",
		Address:   "http://127.0.0.1:18080",
		Rack:      "rack-a",
		Zone:      "zone-a",
		Region:    "region-a",
		Labels:    map[string]string{"disk": "ssd"},
		Capacity:  1024,
		Used:      128,
		Healthy:   true,
		UpdatedAt: now,
	}, resp)
	if resp.Node == nil {
		t.Fatalf("expected registered node in response")
	}
	if resp.Node.ID != "http-node-1" || resp.Node.Address != "http://127.0.0.1:18080" {
		t.Fatalf("unexpected node response: %#v", resp.Node)
	}
	if resp.Node.LastSeenAt == nil {
		t.Fatalf("expected last seen time to be populated")
	}

	stored, err := repo.GetNode(context.Background(), "http-node-1")
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if stored.Address != "http://127.0.0.1:18080" || stored.Capacity != 1024 || !stored.Healthy {
		t.Fatalf("unexpected stored node: %#v", stored)
	}
}

func TestHTTPHandler_HeartbeatNodeAndAllocateUploadTargets(t *testing.T) {
	repo := store.NewMemoryRepository()
	service, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := mds.NewHandler(service)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	router, err := rpc.NewRouter(handler)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	httpHandler, err := rpc.NewHTTPHandler(router, repo, metrics.NewRegistry("mds"))
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	now := time.Now().UTC()
	mustCreateRoot(t, context.Background(), repo, now)

	postRPCAndDecode(t, httpHandler, rpc.MethodRegisterNode, rpc.RegisterNodeRequest{
		ID:        "alloc-node-1",
		Address:   "http://127.0.0.1:28080",
		Capacity:  2048,
		Used:      128,
		Healthy:   true,
		UpdatedAt: now,
	}, &rpc.RegisterNodeResponse{})

	heartbeatResp := &rpc.HeartbeatNodeResponse{}
	postRPCAndDecode(t, httpHandler, rpc.MethodHeartbeatNode, rpc.HeartbeatNodeRequest{
		NodeID:     "alloc-node-1",
		Healthy:    true,
		Capacity:   2048,
		Used:       256,
		LastSeenAt: now.Add(time.Minute),
	}, heartbeatResp)
	if heartbeatResp.Node == nil || heartbeatResp.Node.Used != 256 {
		t.Fatalf("expected updated node usage, got %#v", heartbeatResp.Node)
	}

	getNodeResp := &rpc.GetNodeResponse{}
	postRPCAndDecode(t, httpHandler, rpc.MethodGetNode, rpc.GetNodeRequest{
		ID: "alloc-node-1",
	}, getNodeResp)
	if getNodeResp.Node == nil || getNodeResp.Node.Address != "http://127.0.0.1:28080" {
		t.Fatalf("unexpected get node response: %#v", getNodeResp.Node)
	}

	postRPCAndDecode(t, httpHandler, rpc.MethodCreateFile, rpc.CreateFileRequest{
		InodeID:   "alloc-file-inode",
		FileID:    "alloc-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "alloc.bin",
		Size:      16,
		CreatedAt: now,
	}, &rpc.CreateFileResponse{})

	allocateResp := &rpc.AllocateUploadTargetsResponse{}
	postRPCAndDecode(t, httpHandler, rpc.MethodAllocateUploadTargets, rpc.AllocateUploadTargetsRequest{
		FileID:     "alloc-file",
		ChunkIndex: 0,
	}, allocateResp)
	if len(allocateResp.Targets) != 1 || allocateResp.Targets[0].NodeID != "alloc-node-1" {
		t.Fatalf("unexpected allocation response: %#v", allocateResp)
	}
}

func postRPCAndDecode(t *testing.T, handler http.Handler, method string, request any, response any) {
	t.Helper()

	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal %s request: %v", method, err)
	}
	httpResp := performRequest(t, handler, http.MethodPost, "/rpc/"+method, bytes.NewReader(body))

	if httpResp.StatusCode != http.StatusOK {
		var envelope map[string]any
		_ = json.NewDecoder(httpResp.Body).Decode(&envelope)
		t.Fatalf("expected 200 from %s, got %d with body %#v", method, httpResp.StatusCode, envelope)
	}
	if err := json.NewDecoder(httpResp.Body).Decode(response); err != nil {
		t.Fatalf("decode %s response: %v", method, err)
	}
}

func performRequest(t *testing.T, handler http.Handler, method, path string, body *bytes.Reader) *http.Response {
	t.Helper()

	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		requestBody = body
	}
	req := httptest.NewRequest(method, path, requestBody)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder.Result()
}
