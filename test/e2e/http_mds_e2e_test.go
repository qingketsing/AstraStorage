package e2e_test

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
)

func TestHTTPMDS_E2EUploadAndDownloadPlan(t *testing.T) {
	handler, repo := newHTTPStack(t)
	now := time.Now().UTC()
	ctx := context.Background()
	mustCreateRoot(t, ctx, repo, now)

	postRPCAndDecode(t, handler, rpc.MethodCreateFile, rpc.CreateFileRequest{
		InodeID:   "e2e-inode",
		FileID:    "e2e-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "bundle.bin",
		Size:      metadata.FixedChunkSizeBytes + 128,
		CreatedAt: now,
	}, &rpc.CreateFileResponse{})

	postRPCAndDecode(t, handler, rpc.MethodStartUpload, rpc.StartUploadRequest{
		SessionID:    "e2e-session",
		FileID:       "e2e-file",
		ExpectedSize: metadata.FixedChunkSizeBytes + 128,
		CreatedAt:    now.Add(time.Minute),
	}, &rpc.StartUploadResponse{})

	chunkVerifiedAt := now.Add(90 * time.Second)
	postRPCAndDecode(t, handler, rpc.MethodCommitChunk, rpc.CommitChunkRequest{
		SessionID: "e2e-session",
		ChunkID:   "e2e-chunk-0",
		Index:     0,
		Offset:    0,
		Size:      metadata.FixedChunkSizeBytes,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "e2e-chunk-0",
			Verified:   true,
			VerifiedAt: &chunkVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-a": {
				NodeID: "node-a",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(2 * time.Minute),
	}, &rpc.CommitChunkResponse{})

	postRPCAndDecode(t, handler, rpc.MethodCommitChunk, rpc.CommitChunkRequest{
		SessionID: "e2e-session",
		ChunkID:   "e2e-chunk-1",
		Index:     1,
		Offset:    metadata.FixedChunkSizeBytes,
		Size:      128,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "e2e-chunk-1",
			Verified:   true,
			VerifiedAt: &chunkVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-b": {
				NodeID: "node-b",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(3 * time.Minute),
	}, &rpc.CommitChunkResponse{})

	postRPCAndDecode(t, handler, rpc.MethodCompleteUpload, rpc.CompleteUploadRequest{
		SessionID:        "e2e-session",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusUploading},
		CompletedAt:      now.Add(4 * time.Minute),
	}, &rpc.CompleteUploadResponse{})

	fileVerifiedAt := now.Add(5 * time.Minute)
	verifyResp := &rpc.VerifyUploadResponse{}
	postRPCAndDecode(t, handler, rpc.MethodVerifyUpload, rpc.VerifyUploadRequest{
		SessionID: "e2e-session",
		VerifiedChecksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "e2e-file",
			Verified:   true,
			VerifiedAt: &fileVerifiedAt,
		},
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusVerifying},
		VerifiedAt:       fileVerifiedAt,
	}, verifyResp)
	if verifyResp.File == nil || verifyResp.File.Status != metadata.FileStatusAvailable {
		t.Fatalf("expected available file after verify, got %#v", verifyResp.File)
	}

	planResp := &rpc.BuildDownloadPlanResponse{}
	postRPCAndDecode(t, handler, rpc.MethodBuildDownloadPlan, rpc.BuildDownloadPlanRequest{
		FileID: "e2e-file",
	}, planResp)
	if planResp.Plan == nil || planResp.Plan.ChunkCount != 2 {
		t.Fatalf("expected 2 chunks in download plan, got %#v", planResp.Plan)
	}
	if planResp.Plan.Chunks[0].PreferredNodeID != "node-a" {
		t.Fatalf("expected first chunk preferred node node-a, got %#v", planResp.Plan.Chunks[0])
	}
	if planResp.Plan.Chunks[1].PreferredNodeID != "node-b" {
		t.Fatalf("expected second chunk preferred node node-b, got %#v", planResp.Plan.Chunks[1])
	}
}

func TestHTTPMDS_E2EHealthz(t *testing.T) {
	handler, _ := newHTTPStack(t)
	resp := performRequest(t, handler, http.MethodGet, "/healthz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from healthz, got %d", resp.StatusCode)
	}
}

func newHTTPStack(t *testing.T) (http.Handler, store.Repository) {
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
	httpHandler, err := rpc.NewHTTPHandler(router, repo, metrics.NewRegistry("mds"))
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}
	return httpHandler, repo
}

func mustCreateRoot(t *testing.T, ctx context.Context, repo store.Repository, now time.Time) {
	t.Helper()
	if err := repo.CreateInode(ctx, &metadata.InodeMetadata{
		ID:        metadata.InodeID(metadata.RootInodeID),
		Path:      "/",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create root: %v", err)
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
