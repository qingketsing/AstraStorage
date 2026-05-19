package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"AstraStorage/internal/mds/metadata"
	mdsrpc "AstraStorage/internal/mds/rpc"
	"AstraStorage/internal/platform/observability/logging"
	"AstraStorage/internal/platform/observability/metrics"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"log/slog"
)

func TestHTTPHandler_Healthz(t *testing.T) {
	client, err := newUpstreamClient(Config{
		MDSHTTPBaseURL:  "http://mds.local",
		DataNodeBaseURL: "http://datanode.local",
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"status":"ok"}`))),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})
	if err != nil {
		t.Fatalf("new upstream client: %v", err)
	}
	handler, err := NewHTTPHandler(client, metrics.NewRegistry("gateway"))
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	resp := recorder.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from healthz, got %d", resp.StatusCode)
	}
	var payload healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if payload.Status != "ok" {
		t.Fatalf("expected status ok, got %#v", payload)
	}
	if len(payload.Upstream) != 2 {
		t.Fatalf("expected 2 upstream statuses, got %#v", payload.Upstream)
	}
}

func TestHTTPHandler_RecordsNormalizedRouteMetrics(t *testing.T) {
	handler, registry, err := newGatewayTestHandler(t)
	if err != nil {
		t.Fatalf("new gateway handler: %v", err)
	}

	uploadReq := httptest.NewRequest(http.MethodPost, "/uploads", bytes.NewReader([]byte(`{"content_base64":"not-base64","name":"bundle.bin"}`)))
	uploadReq.Header.Set("Content-Type", "application/json")
	uploadRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadRecorder, uploadReq)
	if uploadRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 from uploads, got %d", uploadRecorder.Code)
	}

	for _, fileID := range []string{"file-1", "file-2"} {
		downloadReq := httptest.NewRequest(http.MethodGet, "/downloads/"+fileID, nil)
		downloadRecorder := httptest.NewRecorder()
		handler.ServeHTTP(downloadRecorder, downloadReq)
		if downloadRecorder.Code != http.StatusBadGateway {
			t.Fatalf("expected 502 from downloads %s, got %d", fileID, downloadRecorder.Code)
		}
		fileReq := httptest.NewRequest(http.MethodDelete, "/files/"+fileID, nil)
		fileRecorder := httptest.NewRecorder()
		handler.ServeHTTP(fileRecorder, fileReq)
		if fileRecorder.Code != http.StatusBadGateway {
			t.Fatalf("expected 502 from files %s, got %d", fileID, fileRecorder.Code)
		}
	}

	unmatchedReq := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	unmatchedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unmatchedRecorder, unmatchedReq)
	if unmatchedRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 from unmatched route, got %d", unmatchedRecorder.Code)
	}

	for _, path := range []string{"/downloads/", "/downloads/a/b", "/files/", "/files/a/b"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("expected 404 from malformed route %s, got %d", path, recorder.Code)
		}
	}

	families := scrapeMetricsFamilies(t, registry)
	requests := metricFamilyByName(t, families, "astrastorage_http_requests_total")
	assertMetricLabels(t, requests.GetMetric(), map[string]string{"service": "gateway", "route": "/uploads", "status_class": "4xx"})
	assertMetricLabels(t, requests.GetMetric(), map[string]string{"service": "gateway", "route": "/downloads/:fileID", "status_class": "5xx"})
	assertMetricValue(t, requests.GetMetric(), map[string]string{"service": "gateway", "route": "/downloads/:fileID", "status_class": "5xx"}, 2)
	assertMetricLabels(t, requests.GetMetric(), map[string]string{"service": "gateway", "route": "/files/:fileID", "status_class": "5xx"})
	assertMetricValue(t, requests.GetMetric(), map[string]string{"service": "gateway", "route": "/files/:fileID", "status_class": "5xx"}, 2)
	assertMetricLabels(t, requests.GetMetric(), map[string]string{"service": "gateway", "route": "/unmatched", "status_class": "4xx"})
	assertMetricValue(t, requests.GetMetric(), map[string]string{"service": "gateway", "route": "/unmatched", "status_class": "4xx"}, 5)
}

func TestHTTPHandler_PoCMetadataAPIs(t *testing.T) {
	calls := make(map[string]int)
	client, err := newUpstreamClient(Config{
		MDSHTTPBaseURL:  "http://mds.local",
		DataNodeBaseURL: "http://datanode.local",
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls[req.URL.String()]++
		switch req.URL.String() {
		case "http://mds.local/rpc/mds.create_directory":
			var payload mdsrpc.CreateDirectoryRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			if payload.InodeID != "dir-1" || payload.ParentID != metadata.InodeID(metadata.RootInodeID) || payload.Name != "docs" {
				t.Fatalf("unexpected create directory request: %#v", payload)
			}
			if payload.CreatedAt.IsZero() {
				t.Fatalf("expected create directory request to include CreatedAt")
			}
			return jsonResponse(req, http.StatusOK, `{"Inode":{"ID":"dir-1","ParentID":"root","Name":"docs","Type":"directory","Status":"active"}}`), nil
		case "http://mds.local/rpc/mds.list_children":
			var payload mdsrpc.ListChildrenRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			if payload.ParentID != "root" || payload.Limit != 10 || payload.Offset != 2 {
				t.Fatalf("unexpected list children request: %#v", payload)
			}
			return jsonResponse(req, http.StatusOK, `{"Entries":[{"ParentID":"root","ChildID":"dir-1","Name":"docs","Type":"directory"}]}`), nil
		case "http://mds.local/rpc/mds.get_file":
			var payload mdsrpc.GetFileRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			if payload.ID != "file-1" {
				t.Fatalf("unexpected get file request: %#v", payload)
			}
			return jsonResponse(req, http.StatusOK, `{"File":{"ID":"file-1","InodeID":"inode-1","Name":"hello.txt","Size":11,"Status":"available"}}`), nil
		case "http://mds.local/rpc/mds.list_file_chunks":
			var payload mdsrpc.ListFileChunksRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			if payload.FileID != "file-1" {
				t.Fatalf("unexpected list file chunks request: %#v", payload)
			}
			return jsonResponse(req, http.StatusOK, `{"Chunks":[{"ID":"chunk-0","FileID":"file-1","Index":0,"Offset":0,"Size":11,"Status":"available"}]}`), nil
		case "http://mds.local/rpc/mds.build_download_plan":
			var payload mdsrpc.BuildDownloadPlanRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			if payload.FileID != "file-1" {
				t.Fatalf("unexpected build download plan request: %#v", payload)
			}
			return jsonResponse(req, http.StatusOK, `{"Plan":{"FileID":"file-1","InodeID":"inode-1","Path":"/hello.txt","Size":11,"StoredSize":11,"ChunkSize":4194304,"FileStatus":"available","ChunkCount":1,"Chunks":[{"ChunkID":"chunk-0","Index":0,"Offset":0,"Size":11,"Status":"available","PreferredNodeID":"node-1","CandidateNodeIDs":["node-1"],"ReplicaCount":1}]}}`), nil
		default:
			return jsonResponse(req, http.StatusInternalServerError, req.URL.String()), nil
		}
	})})
	if err != nil {
		t.Fatalf("new upstream client: %v", err)
	}
	handler, err := NewHTTPHandler(client, metrics.NewRegistry("gateway"))
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	directoryBody := strings.NewReader(`{"inode_id":"dir-1","name":"docs"}`)
	directoryReq := httptest.NewRequest(http.MethodPost, "/directories", directoryBody)
	directoryReq.Header.Set("Content-Type", "application/json")
	directoryRecorder := httptest.NewRecorder()
	handler.ServeHTTP(directoryRecorder, directoryReq)
	if directoryRecorder.Result().StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 from create directory, got %d", directoryRecorder.Result().StatusCode)
	}
	var directoryResp mdsrpc.CreateDirectoryResponse
	if err := json.NewDecoder(directoryRecorder.Result().Body).Decode(&directoryResp); err != nil {
		t.Fatalf("decode create directory response: %v", err)
	}
	if directoryResp.Inode == nil || directoryResp.Inode.ID != "dir-1" || directoryResp.Inode.Name != "docs" {
		t.Fatalf("unexpected create directory response: %#v", directoryResp)
	}

	childrenReq := httptest.NewRequest(http.MethodGet, "/directories/root/children?limit=10&offset=2", nil)
	childrenRecorder := httptest.NewRecorder()
	handler.ServeHTTP(childrenRecorder, childrenReq)
	if childrenRecorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from list children, got %d", childrenRecorder.Result().StatusCode)
	}
	var childrenResp mdsrpc.ListChildrenResponse
	if err := json.NewDecoder(childrenRecorder.Result().Body).Decode(&childrenResp); err != nil {
		t.Fatalf("decode list children response: %v", err)
	}
	if len(childrenResp.Entries) != 1 || childrenResp.Entries[0].ChildID != "dir-1" {
		t.Fatalf("unexpected list children response: %#v", childrenResp)
	}

	fileReq := httptest.NewRequest(http.MethodGet, "/files/file-1", nil)
	fileRecorder := httptest.NewRecorder()
	handler.ServeHTTP(fileRecorder, fileReq)
	if fileRecorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from get file, got %d", fileRecorder.Result().StatusCode)
	}
	var fileResp mdsrpc.GetFileResponse
	if err := json.NewDecoder(fileRecorder.Result().Body).Decode(&fileResp); err != nil {
		t.Fatalf("decode get file response: %v", err)
	}
	if fileResp.File == nil || fileResp.File.ID != "file-1" || fileResp.File.Name != "hello.txt" {
		t.Fatalf("unexpected get file response: %#v", fileResp)
	}

	chunksReq := httptest.NewRequest(http.MethodGet, "/files/file-1/chunks", nil)
	chunksRecorder := httptest.NewRecorder()
	handler.ServeHTTP(chunksRecorder, chunksReq)
	if chunksRecorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from list file chunks, got %d", chunksRecorder.Result().StatusCode)
	}
	var chunksResp mdsrpc.ListFileChunksResponse
	if err := json.NewDecoder(chunksRecorder.Result().Body).Decode(&chunksResp); err != nil {
		t.Fatalf("decode list file chunks response: %v", err)
	}
	if len(chunksResp.Chunks) != 1 || chunksResp.Chunks[0].ID != "chunk-0" {
		t.Fatalf("unexpected list file chunks response: %#v", chunksResp)
	}

	planReq := httptest.NewRequest(http.MethodGet, "/files/file-1/download-plan", nil)
	planRecorder := httptest.NewRecorder()
	handler.ServeHTTP(planRecorder, planReq)
	if planRecorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from download plan, got %d", planRecorder.Result().StatusCode)
	}
	var planResp mdsrpc.BuildDownloadPlanResponse
	if err := json.NewDecoder(planRecorder.Result().Body).Decode(&planResp); err != nil {
		t.Fatalf("decode download plan response: %v", err)
	}
	if planResp.Plan == nil || planResp.Plan.FileID != "file-1" || len(planResp.Plan.Chunks) != 1 {
		t.Fatalf("unexpected download plan response: %#v", planResp)
	}

	for _, url := range []string{
		"http://mds.local/rpc/mds.create_directory",
		"http://mds.local/rpc/mds.list_children",
		"http://mds.local/rpc/mds.get_file",
		"http://mds.local/rpc/mds.list_file_chunks",
		"http://mds.local/rpc/mds.build_download_plan",
	} {
		if calls[url] != 1 {
			t.Fatalf("expected one call to %s, got %d", url, calls[url])
		}
	}
}

func TestHTTPHandler_PoCMetadataAPIValidation(t *testing.T) {
	handler, _, err := newGatewayTestHandler(t)
	if err != nil {
		t.Fatalf("new gateway handler: %v", err)
	}

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{
			name:   "create directory requires name",
			method: http.MethodPost,
			path:   "/directories",
			body:   `{}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "list children rejects negative limit",
			method: http.MethodGet,
			path:   "/directories/root/children?limit=-1",
			want:   http.StatusBadRequest,
		},
		{
			name:   "list children rejects non-integer offset",
			method: http.MethodGet,
			path:   "/directories/root/children?offset=nope",
			want:   http.StatusBadRequest,
		},
		{
			name:   "file chunks only supports get",
			method: http.MethodPost,
			path:   "/files/file-1/chunks",
			want:   http.StatusMethodNotAllowed,
		},
		{
			name:   "download plan only supports get",
			method: http.MethodDelete,
			path:   "/files/file-1/download-plan",
			want:   http.StatusMethodNotAllowed,
		},
		{
			name:   "unknown file subresource",
			method: http.MethodGet,
			path:   "/files/file-1/unknown",
			want:   http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Result().StatusCode != tt.want {
				t.Fatalf("expected status %d, got %d", tt.want, recorder.Result().StatusCode)
			}
		})
	}
}

func TestHTTPHandler_UploadRecordsBusinessMetrics(t *testing.T) {
	var logBuf bytes.Buffer
	oldFactory := newRequestLogger
	newRequestLogger = func(service, component string) *slog.Logger {
		return logging.NewLogger(&logBuf, service, component)
	}
	t.Cleanup(func() { newRequestLogger = oldFactory })

	client, err := newUpstreamClient(Config{
		MDSHTTPBaseURL:  "http://mds.local",
		DataNodeBaseURL: "http://datanode.local",
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "http://mds.local/rpc/mds.create_file",
			"http://mds.local/rpc/mds.start_upload",
			"http://mds.local/rpc/mds.complete_upload",
			"http://mds.local/rpc/mds.verify_upload":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://mds.local/rpc/mds.allocate_upload_targets":
			var payload mdsrpc.AllocateUploadTargetsRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			addr := "http://node-1.local"
			if payload.ChunkIndex == 1 {
				addr = "http://node-2.local"
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewReader([]byte(`{
					"FileID":"file-1",
					"ChunkIndex":` + jsonNumber(payload.ChunkIndex) + `,
					"Targets":[{"NodeID":"node-1","Address":"` + addr + `"}]
				}`))),
				Header:  make(http.Header),
				Request: req,
			}, nil
		case "http://mds.local/rpc/mds.commit_chunk":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://node-1.local/chunks/chunk-seq-0":
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://node-2.local/chunks/chunk-seq-1":
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader([]byte(req.URL.String()))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
	})})
	if err != nil {
		t.Fatalf("new upstream client: %v", err)
	}
	registry := metrics.NewRegistry("gateway")
	handler, err := NewHTTPHandler(client, registry)
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	content := append(bytes.Repeat([]byte("a"), int(metadata.FixedChunkSizeBytes)), []byte("tail")...)
	body, err := json.Marshal(map[string]any{
		"file_id":        "file-1",
		"inode_id":       "inode-1",
		"session_id":     "session-1",
		"chunk_id":       "chunk-seq",
		"parent_id":      "root",
		"name":           "big.bin",
		"content_base64": base64.StdEncoding.EncodeToString(content),
	})
	if err != nil {
		t.Fatalf("marshal upload body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/uploads", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(logging.RequestIDHeader, "req-upload")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 from upload, got %d", recorder.Result().StatusCode)
	}

	families := scrapeMetricsFamilies(t, registry)
	uploadRequests := metricFamilyByName(t, families, "astrastorage_gateway_upload_requests_total")
	assertMetricValue(t, uploadRequests.GetMetric(), map[string]string{"result": "success"}, 1)

	uploadChunks := metricFamilyByName(t, families, "astrastorage_gateway_upload_chunks_total")
	assertMetricValue(t, uploadChunks.GetMetric(), map[string]string{"result": "success"}, 2)

	uploadBytes := metricFamilyByName(t, families, "astrastorage_gateway_upload_bytes_total")
	assertMetricValue(t, uploadBytes.GetMetric(), map[string]string{}, float64(len(content)))

	if !strings.Contains(logBuf.String(), "req-upload") {
		t.Fatalf("expected upload log to include request id, got %q", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "\"file_id\":\"file-1\"") {
		t.Fatalf("expected upload log to include file_id, got %q", logBuf.String())
	}
}

func TestHTTPHandler_UploadPropagatesRequestIDAndRecordsOutboundMetrics(t *testing.T) {
	requestIDs := make(map[string]string)
	client, err := newUpstreamClient(Config{
		MDSHTTPBaseURL:  "http://mds.local",
		DataNodeBaseURL: "http://datanode.local",
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestIDs[req.URL.String()] = req.Header.Get(logging.RequestIDHeader)
		switch req.URL.String() {
		case "http://mds.local/rpc/mds.create_file",
			"http://mds.local/rpc/mds.start_upload",
			"http://mds.local/rpc/mds.commit_chunk",
			"http://mds.local/rpc/mds.complete_upload",
			"http://mds.local/rpc/mds.verify_upload":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://mds.local/rpc/mds.allocate_upload_targets":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewReader([]byte(`{
					"FileID":"file-1",
					"ChunkIndex":0,
					"Targets":[{"NodeID":"node-1","Address":"http://node-1.local"}]
				}`))),
				Header:  make(http.Header),
				Request: req,
			}, nil
		case "http://node-1.local/chunks/chunk-1":
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader([]byte(req.URL.String()))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
	})})
	if err != nil {
		t.Fatalf("new upstream client: %v", err)
	}
	registry := metrics.NewRegistry("gateway")
	handler, err := NewHTTPHandler(client, registry)
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	body, err := json.Marshal(map[string]any{
		"file_id":        "file-1",
		"inode_id":       "inode-1",
		"session_id":     "session-1",
		"chunk_id":       "chunk-1",
		"parent_id":      "root",
		"name":           "hello.txt",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("hello gateway")),
	})
	if err != nil {
		t.Fatalf("marshal upload body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/uploads", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(logging.RequestIDHeader, "req-outbound")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 from upload, got %d", recorder.Result().StatusCode)
	}

	for _, url := range []string{
		"http://mds.local/rpc/mds.create_file",
		"http://mds.local/rpc/mds.start_upload",
		"http://mds.local/rpc/mds.allocate_upload_targets",
		"http://mds.local/rpc/mds.commit_chunk",
		"http://mds.local/rpc/mds.complete_upload",
		"http://mds.local/rpc/mds.verify_upload",
		"http://node-1.local/chunks/chunk-1",
	} {
		if got := requestIDs[url]; got != "req-outbound" {
			t.Fatalf("expected request id req-outbound for %s, got %q", url, got)
		}
	}

	families := scrapeMetricsFamilies(t, registry)
	upstreamRequests := metricFamilyByName(t, families, "astrastorage_gateway_upstream_requests_total")
	assertMetricValue(t, upstreamRequests.GetMetric(), map[string]string{
		"target":    "mds",
		"operation": "mds.start_upload",
		"result":    "success",
	}, 1)
	assertMetricValue(t, upstreamRequests.GetMetric(), map[string]string{
		"target":    "datanode",
		"operation": "datanode.put_chunk",
		"result":    "success",
	}, 1)

	upstreamDuration := metricFamilyByName(t, families, "astrastorage_gateway_upstream_request_duration_seconds")
	assertHistogramCount(t, upstreamDuration.GetMetric(), map[string]string{
		"target":    "mds",
		"operation": "mds.start_upload",
		"result":    "success",
	}, 1)
	assertHistogramCount(t, upstreamDuration.GetMetric(), map[string]string{
		"target":    "datanode",
		"operation": "datanode.put_chunk",
		"result":    "success",
	}, 1)
}

func TestUpstreamClient_DeleteChunkNotFoundRecordsSuccess(t *testing.T) {
	client, err := newUpstreamClient(Config{
		MDSHTTPBaseURL:  "http://mds.local",
		DataNodeBaseURL: "http://datanode.local",
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})
	if err != nil {
		t.Fatalf("new upstream client: %v", err)
	}
	registry := metrics.NewRegistry("gateway")
	obs, err := newGatewayObservability(registry)
	if err != nil {
		t.Fatalf("new observability: %v", err)
	}
	client.obs = obs

	if err := client.DeleteChunk(context.Background(), "http://node-1.local", "chunk-404"); err != nil {
		t.Fatalf("delete chunk should treat not found as success, got %v", err)
	}

	families := scrapeMetricsFamilies(t, registry)
	upstreamRequests := metricFamilyByName(t, families, "astrastorage_gateway_upstream_requests_total")
	assertMetricValue(t, upstreamRequests.GetMetric(), map[string]string{
		"target":    "datanode",
		"operation": "datanode.delete_chunk",
		"result":    "success",
	}, 1)
}

func TestHTTPHandler_UploadPostCommitFailureRecordsChunkFailure(t *testing.T) {
	client, err := newUpstreamClient(Config{
		MDSHTTPBaseURL:  "http://mds.local",
		DataNodeBaseURL: "http://datanode.local",
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "http://mds.local/rpc/mds.create_file",
			"http://mds.local/rpc/mds.start_upload",
			"http://mds.local/rpc/mds.commit_chunk":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://mds.local/rpc/mds.allocate_upload_targets":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewReader([]byte(`{
					"FileID":"file-1",
					"ChunkIndex":0,
					"Targets":[{"NodeID":"node-1","Address":"http://node-1.local"}]
				}`))),
				Header:  make(http.Header),
				Request: req,
			}, nil
		case "http://node-1.local/chunks/chunk-1":
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://mds.local/rpc/mds.complete_upload":
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       io.NopCloser(bytes.NewReader([]byte(`boom`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader([]byte(req.URL.String()))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
	})})
	if err != nil {
		t.Fatalf("new upstream client: %v", err)
	}
	registry := metrics.NewRegistry("gateway")
	handler, err := NewHTTPHandler(client, registry)
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	body, err := json.Marshal(map[string]any{
		"file_id":        "file-1",
		"inode_id":       "inode-1",
		"session_id":     "session-1",
		"chunk_id":       "chunk-1",
		"parent_id":      "root",
		"name":           "partial.bin",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("partial upload")),
	})
	if err != nil {
		t.Fatalf("marshal upload body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/uploads", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 from upload failure, got %d", recorder.Result().StatusCode)
	}

	families := scrapeMetricsFamilies(t, registry)
	uploadRequests := metricFamilyByName(t, families, "astrastorage_gateway_upload_requests_total")
	assertMetricValue(t, uploadRequests.GetMetric(), map[string]string{"result": "failure"}, 1)

	uploadChunks := metricFamilyByName(t, families, "astrastorage_gateway_upload_chunks_total")
	assertMetricValue(t, uploadChunks.GetMetric(), map[string]string{"result": "failure"}, 1)
}

func TestHTTPHandler_DownloadFailureRecordsBusinessMetrics(t *testing.T) {
	handler, registry, err := newGatewayTestHandler(t)
	if err != nil {
		t.Fatalf("new gateway handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/downloads/file-1", nil)
	req.Header.Set(logging.RequestIDHeader, "req-download")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 from download, got %d", recorder.Result().StatusCode)
	}

	families := scrapeMetricsFamilies(t, registry)
	downloadRequests := metricFamilyByName(t, families, "astrastorage_gateway_download_requests_total")
	assertMetricValue(t, downloadRequests.GetMetric(), map[string]string{"result": "failure"}, 1)
}

func TestHTTPHandler_DeleteRecordsBusinessMetrics(t *testing.T) {
	client, err := newUpstreamClient(Config{
		MDSHTTPBaseURL:  "http://mds.local",
		DataNodeBaseURL: "http://datanode.local",
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "http://mds.local/rpc/mds.list_file_chunks":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewReader([]byte(`{
					"Chunks":[
						{
							"ID":"chunk-0",
							"FileID":"file-1",
							"Index":0,
							"Offset":0,
							"Size":11,
							"Replicas":{
								"node-1":{"NodeID":"node-1","Role":"primary","State":"ready"}
							}
						}
					]
				}`))),
				Header:  make(http.Header),
				Request: req,
			}, nil
		case "http://mds.local/rpc/mds.get_node":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"Node":{"ID":"node-1","Address":"http://node-1.local"}}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://mds.local/rpc/mds.delete_file":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://node-1.local/chunks/chunk-0":
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(bytes.NewReader(nil)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader([]byte(req.URL.String()))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
	})})
	if err != nil {
		t.Fatalf("new upstream client: %v", err)
	}
	registry := metrics.NewRegistry("gateway")
	handler, err := NewHTTPHandler(client, registry)
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/files/file-1", nil)
	req.Header.Set(logging.RequestIDHeader, "req-delete")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 from delete file, got %d", recorder.Result().StatusCode)
	}

	families := scrapeMetricsFamilies(t, registry)
	deleteRequests := metricFamilyByName(t, families, "astrastorage_gateway_delete_requests_total")
	assertMetricValue(t, deleteRequests.GetMetric(), map[string]string{"result": "success"}, 1)
}

func TestHTTPHandler_ReusesInboundRequestID(t *testing.T) {
	var logBuf bytes.Buffer
	oldFactory := newRequestLogger
	newRequestLogger = func(service, component string) *slog.Logger {
		return logging.NewLogger(&logBuf, service, component)
	}
	t.Cleanup(func() { newRequestLogger = oldFactory })

	innerSeen := ""
	mux := http.NewServeMux()
	mux.HandleFunc("/uploads", func(w http.ResponseWriter, r *http.Request) {
		innerSeen = logging.RequestIDFromContext(r.Context())
		if innerSeen == "" {
			t.Fatalf("expected request id in context")
		}
		logging.SetRequestIDHeader(w.Header(), innerSeen)
		w.WriteHeader(http.StatusNoContent)
	})
	handler := &httpHandler{
		registry: metrics.NewRegistry("gateway"),
		logger:   newRequestLogger("gateway", "http"),
		mux:      mux,
	}

	req := httptest.NewRequest(http.MethodPost, "/uploads", nil)
	req.Header.Set(logging.RequestIDHeader, "req-fixed")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got := recorder.Header().Get(logging.RequestIDHeader); got != "req-fixed" {
		t.Fatalf("expected request id to be reused, got %q", got)
	}
	if innerSeen != "req-fixed" {
		t.Fatalf("expected inner handler to see request id req-fixed, got %q", innerSeen)
	}
	if !strings.Contains(logBuf.String(), "req-fixed") {
		t.Fatalf("expected request id in log output, got %q", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "\"route\":\"/uploads\"") {
		t.Fatalf("expected normalized route in log output, got %q", logBuf.String())
	}
}

func TestHTTPHandler_AssignsRequestIDWhenMissing(t *testing.T) {
	var logBuf bytes.Buffer
	oldFactory := newRequestLogger
	newRequestLogger = func(service, component string) *slog.Logger {
		return logging.NewLogger(&logBuf, service, component)
	}
	t.Cleanup(func() { newRequestLogger = oldFactory })

	innerSeen := ""
	mux := http.NewServeMux()
	mux.HandleFunc("/uploads", func(w http.ResponseWriter, r *http.Request) {
		innerSeen = logging.RequestIDFromContext(r.Context())
		if innerSeen == "" {
			t.Fatalf("expected request id in context")
		}
		logging.SetRequestIDHeader(w.Header(), innerSeen)
		w.WriteHeader(http.StatusNoContent)
	})
	handler := &httpHandler{
		registry: metrics.NewRegistry("gateway"),
		logger:   newRequestLogger("gateway", "http"),
		mux:      mux,
	}

	req := httptest.NewRequest(http.MethodPost, "/uploads", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	requestID := recorder.Header().Get(logging.RequestIDHeader)
	if requestID == "" {
		t.Fatalf("expected generated request id response header to be set")
	}
	if innerSeen != requestID {
		t.Fatalf("expected inner handler to see generated request id %q, got %q", requestID, innerSeen)
	}
	if !strings.Contains(logBuf.String(), requestID) {
		t.Fatalf("expected generated request id in log output, got %q", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "\"route\":\"/uploads\"") {
		t.Fatalf("expected normalized route in log output, got %q", logBuf.String())
	}
}

func TestHTTPHandler_UploadFlow(t *testing.T) {
	client, err := newUpstreamClient(Config{
		MDSHTTPBaseURL:  "http://mds.local",
		DataNodeBaseURL: "http://datanode.local",
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "http://mds.local/rpc/mds.create_file",
			"http://mds.local/rpc/mds.start_upload",
			"http://mds.local/rpc/mds.commit_chunk",
			"http://mds.local/rpc/mds.complete_upload",
			"http://mds.local/rpc/mds.verify_upload":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://mds.local/rpc/mds.allocate_upload_targets":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewReader([]byte(`{
					"FileID":"file-1",
					"ChunkIndex":0,
					"Targets":[{"NodeID":"node-1","Address":"http://node-1.local"}]
				}`))),
				Header:  make(http.Header),
				Request: req,
			}, nil
		case "http://node-1.local/chunks/chunk-1":
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader([]byte(req.URL.String()))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
	})})
	if err != nil {
		t.Fatalf("new upstream client: %v", err)
	}
	handler, err := NewHTTPHandler(client, metrics.NewRegistry("gateway"))
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	body, err := json.Marshal(map[string]any{
		"file_id":        "file-1",
		"inode_id":       "inode-1",
		"session_id":     "session-1",
		"chunk_id":       "chunk-1",
		"parent_id":      "root",
		"name":           "hello.txt",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("hello gateway")),
	})
	if err != nil {
		t.Fatalf("marshal upload body: %v", err)
	}
	uploadReq := httptest.NewRequest(http.MethodPost, "/uploads", bytes.NewReader(body))
	uploadReq.Header.Set("Content-Type", "application/json")
	uploadRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadRecorder, uploadReq)
	if uploadRecorder.Result().StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 from uploads, got %d", uploadRecorder.Result().StatusCode)
	}

	var uploadResp uploadResponse
	if err := json.NewDecoder(uploadRecorder.Result().Body).Decode(&uploadResp); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploadResp.NodeID != "node-1" || uploadResp.NodeAddress != "http://node-1.local" {
		t.Fatalf("unexpected upload response: %#v", uploadResp)
	}
	if uploadResp.ChunkCount != 1 || len(uploadResp.Chunks) != 1 {
		t.Fatalf("expected single uploaded chunk, got %#v", uploadResp)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/downloads/file-1", nil)
	downloadRecorder := httptest.NewRecorder()
	handler.ServeHTTP(downloadRecorder, downloadReq)
	if downloadRecorder.Result().StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 from downloads without plan stubs, got %d", downloadRecorder.Result().StatusCode)
	}
}

func newGatewayTestHandler(t *testing.T) (http.Handler, *metrics.Registry, error) {
	t.Helper()
	client, err := newUpstreamClient(Config{
		MDSHTTPBaseURL:  "http://mds.local",
		DataNodeBaseURL: "http://datanode.local",
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader(`boom`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})
	if err != nil {
		return nil, nil, err
	}
	registry := metrics.NewRegistry("gateway")
	handler, err := NewHTTPHandler(client, registry)
	if err != nil {
		return nil, nil, err
	}
	return handler, registry, nil
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
	return metricFamiliesSlice(families)
}

func metricFamiliesSlice(families map[string]*dto.MetricFamily) []*dto.MetricFamily {
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

func assertHistogramCount(t *testing.T, metrics []*dto.Metric, want map[string]string, count uint64) {
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

func TestHTTPHandler_DownloadFlowWithFallback(t *testing.T) {
	client, err := newUpstreamClient(Config{
		MDSHTTPBaseURL:  "http://mds.local",
		DataNodeBaseURL: "http://datanode.local",
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "http://mds.local/rpc/mds.build_download_plan":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewReader([]byte(`{
					"Plan":{
						"FileID":"file-1",
						"Path":"/hello.txt",
						"Size":11,
						"ChunkCount":2,
						"Chunks":[
							{"ChunkID":"chunk-0","Index":0,"PreferredNodeID":"node-1","CandidateNodeIDs":["node-1"]},
							{"ChunkID":"chunk-1","Index":1,"PreferredNodeID":"node-1","CandidateNodeIDs":["node-1","node-2"]}
						]
					}
				}`))),
				Header:  make(http.Header),
				Request: req,
			}, nil
		case "http://mds.local/rpc/mds.get_node":
			var payload map[string]string
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			switch payload["ID"] {
			case "node-1":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte(`{"Node":{"ID":"node-1","Address":"http://node-1.local"}}`))),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			case "node-2":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte(`{"Node":{"ID":"node-2","Address":"http://node-2.local"}}`))),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}
		case "http://node-1.local/chunks/chunk-0":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("hello "))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://node-1.local/chunks/chunk-1":
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader([]byte("boom"))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://node-2.local/chunks/chunk-1":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("world"))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader([]byte(req.URL.String()))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
	})})
	if err != nil {
		t.Fatalf("new upstream client: %v", err)
	}
	handler, err := NewHTTPHandler(client, metrics.NewRegistry("gateway"))
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/downloads/file-1", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	resp := recorder.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from downloads, got %d", resp.StatusCode)
	}
	if got := recorder.Body.String(); got != "hello world" {
		t.Fatalf("expected concatenated body %q, got %q", "hello world", got)
	}
}

func TestHTTPHandler_DeleteFileFlow(t *testing.T) {
	var deletedChunkURLs []string
	var deleteFileCalled bool

	client, err := newUpstreamClient(Config{
		MDSHTTPBaseURL:  "http://mds.local",
		DataNodeBaseURL: "http://datanode.local",
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "http://mds.local/rpc/mds.list_file_chunks":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewReader([]byte(`{
					"Chunks":[
						{
							"ID":"chunk-0",
							"FileID":"file-1",
							"Index":0,
							"Offset":0,
							"Size":11,
							"Replicas":{
								"node-1":{"NodeID":"node-1","Role":"primary","State":"ready"},
								"node-2":{"NodeID":"node-2","Role":"secondary","State":"pending"}
							}
						}
					]
				}`))),
				Header:  make(http.Header),
				Request: req,
			}, nil
		case "http://mds.local/rpc/mds.get_node":
			var payload map[string]string
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			switch payload["ID"] {
			case "node-1":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte(`{"Node":{"ID":"node-1","Address":"http://node-1.local"}}`))),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			case "node-2":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte(`{"Node":{"ID":"node-2","Address":"http://node-2.local"}}`))),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}
		case "http://mds.local/rpc/mds.delete_file":
			deleteFileCalled = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://node-1.local/chunks/chunk-0":
			deletedChunkURLs = append(deletedChunkURLs, req.URL.String())
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(bytes.NewReader(nil)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://node-2.local/chunks/chunk-0":
			deletedChunkURLs = append(deletedChunkURLs, req.URL.String())
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewReader(nil)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader([]byte(req.URL.String()))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
	})})
	if err != nil {
		t.Fatalf("new upstream client: %v", err)
	}
	handler, err := NewHTTPHandler(client, metrics.NewRegistry("gateway"))
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/files/file-1", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	resp := recorder.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 from delete file, got %d", resp.StatusCode)
	}
	if !deleteFileCalled {
		t.Fatalf("expected mds delete file to be called")
	}
	if len(deletedChunkURLs) != 2 {
		t.Fatalf("expected delete chunk calls for both replicas, got %#v", deletedChunkURLs)
	}
}

func TestHTTPHandler_MultiChunkUploadFlow(t *testing.T) {
	var allocatedIndexes []int64
	var committedIndexes []int64
	var committedOffsets []int64
	var putChunkURLs []string

	client, err := newUpstreamClient(Config{
		MDSHTTPBaseURL:  "http://mds.local",
		DataNodeBaseURL: "http://datanode.local",
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "http://mds.local/rpc/mds.create_file",
			"http://mds.local/rpc/mds.start_upload",
			"http://mds.local/rpc/mds.complete_upload",
			"http://mds.local/rpc/mds.verify_upload":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://mds.local/rpc/mds.allocate_upload_targets":
			var payload mdsrpc.AllocateUploadTargetsRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			allocatedIndexes = append(allocatedIndexes, payload.ChunkIndex)
			nodeID := "node-1"
			nodeAddr := "http://node-1.local"
			if payload.ChunkIndex == 1 {
				nodeID = "node-2"
				nodeAddr = "http://node-2.local"
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewReader([]byte(`{
					"FileID":"file-1",
					"ChunkIndex":` + jsonNumber(payload.ChunkIndex) + `,
					"Targets":[{"NodeID":"` + nodeID + `","Address":"` + nodeAddr + `"}]
				}`))),
				Header:  make(http.Header),
				Request: req,
			}, nil
		case "http://mds.local/rpc/mds.commit_chunk":
			var payload mdsrpc.CommitChunkRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			committedIndexes = append(committedIndexes, payload.Index)
			committedOffsets = append(committedOffsets, payload.Offset)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://node-1.local/chunks/chunk-seq-0", "http://node-2.local/chunks/chunk-seq-1":
			putChunkURLs = append(putChunkURLs, req.URL.String())
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader([]byte(req.URL.String()))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
	})})
	if err != nil {
		t.Fatalf("new upstream client: %v", err)
	}
	handler, err := NewHTTPHandler(client, metrics.NewRegistry("gateway"))
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	content := append(bytes.Repeat([]byte("a"), int(metadata.FixedChunkSizeBytes)), []byte("tail")...)
	body, err := json.Marshal(map[string]any{
		"file_id":        "file-1",
		"inode_id":       "inode-1",
		"session_id":     "session-1",
		"chunk_id":       "chunk-seq",
		"parent_id":      "root",
		"name":           "big.bin",
		"content_base64": base64.StdEncoding.EncodeToString(content),
	})
	if err != nil {
		t.Fatalf("marshal upload body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/uploads", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	resp := recorder.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 from multi chunk upload, got %d", resp.StatusCode)
	}

	var payload uploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if payload.ChunkCount != 2 || len(payload.Chunks) != 2 {
		t.Fatalf("expected 2 uploaded chunks, got %#v", payload)
	}
	if payload.Chunks[0].ChunkID != "chunk-seq-0" || payload.Chunks[1].ChunkID != "chunk-seq-1" {
		t.Fatalf("unexpected chunk IDs: %#v", payload.Chunks)
	}
	if payload.Chunks[1].Offset != metadata.FixedChunkSizeBytes {
		t.Fatalf("expected second chunk offset %d, got %d", metadata.FixedChunkSizeBytes, payload.Chunks[1].Offset)
	}
	if !equalInt64Slices(allocatedIndexes, []int64{0, 1}) {
		t.Fatalf("unexpected allocated indexes: %#v", allocatedIndexes)
	}
	if !equalInt64Slices(committedIndexes, []int64{0, 1}) {
		t.Fatalf("unexpected committed indexes: %#v", committedIndexes)
	}
	if !equalInt64Slices(committedOffsets, []int64{0, metadata.FixedChunkSizeBytes}) {
		t.Fatalf("unexpected committed offsets: %#v", committedOffsets)
	}
	if len(putChunkURLs) != 2 {
		t.Fatalf("expected 2 put chunk calls, got %#v", putChunkURLs)
	}
}

func TestHTTPHandler_UploadFlowWithReplicaForwarding(t *testing.T) {
	var committedReplicas []metadata.ReplicaSet

	client, err := newUpstreamClient(Config{
		MDSHTTPBaseURL:  "http://mds.local",
		DataNodeBaseURL: "http://datanode.local",
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "http://mds.local/rpc/mds.create_file",
			"http://mds.local/rpc/mds.start_upload",
			"http://mds.local/rpc/mds.complete_upload",
			"http://mds.local/rpc/mds.verify_upload":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://mds.local/rpc/mds.allocate_upload_targets":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewReader([]byte(`{
					"FileID":"file-1",
					"ChunkIndex":0,
					"Targets":[
						{"NodeID":"node-1","Address":"http://node-1.local"},
						{"NodeID":"node-2","Address":"http://node-2.local"},
						{"NodeID":"node-3","Address":"http://node-3.local"}
					]
				}`))),
				Header:  make(http.Header),
				Request: req,
			}, nil
		case "http://mds.local/rpc/mds.commit_chunk":
			var payload mdsrpc.CommitChunkRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			committedReplicas = append(committedReplicas, payload.Replicas)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://node-1.local/chunks/chunk-1":
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"chunk":{"chunk_id":"chunk-1"}}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://node-1.local/internal/replicate":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewReader([]byte(`{
						"replicas":[
							{"node_id":"node-2","state":"ready"},
							{"node_id":"node-3","state":"pending","error":"status 500"}
						]
				}`))),
				Header:  make(http.Header),
				Request: req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader([]byte(req.URL.String()))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
	})})
	if err != nil {
		t.Fatalf("new upstream client: %v", err)
	}
	handler, err := NewHTTPHandler(client, metrics.NewRegistry("gateway"))
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	body, err := json.Marshal(map[string]any{
		"file_id":        "file-1",
		"inode_id":       "inode-1",
		"session_id":     "session-1",
		"chunk_id":       "chunk-1",
		"parent_id":      "root",
		"name":           "replicas.txt",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("replica payload")),
	})
	if err != nil {
		t.Fatalf("marshal upload body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/uploads", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	resp := recorder.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 from replica forwarding upload, got %d", resp.StatusCode)
	}
	if len(committedReplicas) != 1 {
		t.Fatalf("expected 1 committed chunk, got %d", len(committedReplicas))
	}
	replicas := committedReplicas[0]
	if len(replicas) != 3 {
		t.Fatalf("expected 3 replicas in commit request, got %#v", replicas)
	}
	if replicas["node-1"].Role != metadata.ReplicaRolePrimary || replicas["node-1"].State != metadata.ReplicaStateReady {
		t.Fatalf("unexpected primary replica: %#v", replicas["node-1"])
	}
	if replicas["node-2"].Role != metadata.ReplicaRoleSecondary || replicas["node-2"].State != metadata.ReplicaStateReady {
		t.Fatalf("unexpected ready secondary replica: %#v", replicas["node-2"])
	}
	if replicas["node-3"].Role != metadata.ReplicaRoleSecondary || replicas["node-3"].State != metadata.ReplicaStatePending {
		t.Fatalf("unexpected pending secondary replica: %#v", replicas["node-3"])
	}
}

func equalInt64Slices(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func jsonNumber(v int64) string {
	return strconv.FormatInt(v, 10)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}
}
