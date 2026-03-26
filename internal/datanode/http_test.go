package datanode

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"AstraStorage/internal/platform/observability/logging"
	"AstraStorage/internal/platform/observability/metrics"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"log/slog"
)

func TestHTTPHandler_PutGetDeleteChunk(t *testing.T) {
	store, err := NewStore(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	handler, err := NewHTTPHandler(store, metrics.NewRegistry("datanode"))
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	data := []byte("chunk over http")
	sum := sha256.Sum256(data)

	putReq := httptest.NewRequest(http.MethodPut, "/chunks/chunk-http", bytes.NewReader(data))
	putReq.Header.Set("X-File-ID", "file-http")
	putReq.Header.Set("X-Checksum-Algorithm", "sha256")
	putReq.Header.Set("X-Checksum-Value", hex.EncodeToString(sum[:]))
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, putReq)
	putResp := putRecorder.Result()
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 from put chunk, got %d", putResp.StatusCode)
	}
	var putBody putChunkResponse
	if err := json.NewDecoder(putResp.Body).Decode(&putBody); err != nil {
		t.Fatalf("decode put response: %v", err)
	}
	if putBody.Chunk == nil || putBody.Chunk.ChunkID != "chunk-http" {
		t.Fatalf("expected stored chunk metadata, got %#v", putBody.Chunk)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/chunks/chunk-http", nil)
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	getResp := getRecorder.Result()
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from get chunk, got %d", getResp.StatusCode)
	}
	if got := getRecorder.Body.String(); got != string(data) {
		t.Fatalf("expected chunk data %q, got %q", data, got)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/chunks/chunk-http", nil)
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteReq)
	deleteResp := deleteRecorder.Result()
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 from delete chunk, got %d", deleteResp.StatusCode)
	}
}

func TestHTTPHandler_Healthz(t *testing.T) {
	store, err := NewStore(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	handler, err := NewHTTPHandler(store, metrics.NewRegistry("datanode"))
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
}

func TestHTTPHandler_RecordsNormalizedRouteMetrics(t *testing.T) {
	handler, registry, err := newDataNodeTestHandler(t)
	if err != nil {
		t.Fatalf("new datanode handler: %v", err)
	}

	for _, chunkID := range []string{"chunk-1", "chunk-2"} {
		putReq := httptest.NewRequest(http.MethodPut, "/chunks/"+chunkID, bytes.NewReader([]byte("chunk-data")))
		putRecorder := httptest.NewRecorder()
		handler.ServeHTTP(putRecorder, putReq)
		if putRecorder.Code != http.StatusCreated {
			t.Fatalf("expected 201 from chunks %s, got %d", chunkID, putRecorder.Code)
		}
	}

	replicateReq := httptest.NewRequest(http.MethodPost, "/internal/replicate", bytes.NewReader([]byte(`{"broken":`)))
	replicateReq.Header.Set("Content-Type", "application/json")
	replicateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(replicateRecorder, replicateReq)
	if replicateRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 from internal replicate, got %d", replicateRecorder.Code)
	}

	unmatchedReq := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	unmatchedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unmatchedRecorder, unmatchedReq)
	if unmatchedRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 from unmatched route, got %d", unmatchedRecorder.Code)
	}

	for _, path := range []string{"/chunks/", "/chunks/a/b"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("expected 404 from malformed route %s, got %d", path, recorder.Code)
		}
	}

	families := scrapeMetricsFamilies(t, registry)
	requests := metricFamilyByName(t, families, "astrastorage_http_requests_total")
	assertMetricLabels(t, requests.GetMetric(), map[string]string{"service": "datanode", "route": "/chunks/:chunkID", "status_class": "2xx"})
	assertMetricValue(t, requests.GetMetric(), map[string]string{"service": "datanode", "route": "/chunks/:chunkID", "status_class": "2xx"}, 2)
	assertMetricLabels(t, requests.GetMetric(), map[string]string{"service": "datanode", "route": "/internal/replicate", "status_class": "4xx"})
	assertMetricValue(t, requests.GetMetric(), map[string]string{"service": "datanode", "route": "/internal/replicate", "status_class": "4xx"}, 1)
	assertMetricLabels(t, requests.GetMetric(), map[string]string{"service": "datanode", "route": "/unmatched", "status_class": "4xx"})
	assertMetricValue(t, requests.GetMetric(), map[string]string{"service": "datanode", "route": "/unmatched", "status_class": "4xx"}, 3)
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
	mux.HandleFunc("/chunks/", func(w http.ResponseWriter, r *http.Request) {
		innerSeen = logging.RequestIDFromContext(r.Context())
		if innerSeen == "" {
			t.Fatalf("expected request id in context")
		}
		logging.SetRequestIDHeader(w.Header(), innerSeen)
		w.WriteHeader(http.StatusCreated)
	})
	handler := &httpHandler{
		registry: metrics.NewRegistry("datanode"),
		logger:   newRequestLogger("datanode", "http"),
		mux:      mux,
	}

	req := httptest.NewRequest(http.MethodPut, "/chunks/chunk-1", nil)
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
	if !strings.Contains(logBuf.String(), "\"route\":\"/chunks/:chunkID\"") {
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
	mux.HandleFunc("/chunks/", func(w http.ResponseWriter, r *http.Request) {
		innerSeen = logging.RequestIDFromContext(r.Context())
		if innerSeen == "" {
			t.Fatalf("expected request id in context")
		}
		logging.SetRequestIDHeader(w.Header(), innerSeen)
		w.WriteHeader(http.StatusCreated)
	})
	handler := &httpHandler{
		registry: metrics.NewRegistry("datanode"),
		logger:   newRequestLogger("datanode", "http"),
		mux:      mux,
	}

	req := httptest.NewRequest(http.MethodPut, "/chunks/chunk-1", nil)
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
	if !strings.Contains(logBuf.String(), "\"route\":\"/chunks/:chunkID\"") {
		t.Fatalf("expected normalized route in log output, got %q", logBuf.String())
	}
}

func TestHTTPHandler_PutChunkForwardsReplicas(t *testing.T) {
	secondaryData := make(map[string][]byte)

	store, err := NewStore(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	handler, err := newHTTPHandler(store, metrics.NewRegistry("datanode"), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		switch req.URL.String() {
		case "http://node-2.local/chunks/chunk-forward":
			secondaryData[req.URL.Path] = body
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"chunk":{"chunk_id":"chunk-forward"}}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://node-3.local/chunks/chunk-forward":
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader([]byte(`boom`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewReader([]byte(req.URL.String()))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
	})})
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	data := []byte("replicated chunk")
	sum := sha256.Sum256(data)

	putReq := httptest.NewRequest(http.MethodPut, "/chunks/chunk-forward", bytes.NewReader(data))
	putReq.Header.Set("X-File-ID", "file-forward")
	putReq.Header.Set("X-Checksum-Algorithm", "sha256")
	putReq.Header.Set("X-Checksum-Value", hex.EncodeToString(sum[:]))
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, putReq)
	if putRecorder.Result().StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 from initial put chunk, got %d", putRecorder.Result().StatusCode)
	}

	replicateBody, err := json.Marshal(ReplicateChunkRequest{
		ChunkID: "chunk-forward",
		Targets: []ReplicaTarget{
			{NodeID: "node-2", Address: "http://node-2.local"},
			{NodeID: "node-3", Address: "http://node-3.local"},
		},
	})
	if err != nil {
		t.Fatalf("marshal replicate body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/internal/replicate", bytes.NewReader(replicateBody))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	resp := recorder.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from internal replicate, got %d", resp.StatusCode)
	}
	var payload ReplicateChunkResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode replicate response: %v", err)
	}
	if len(payload.Replicas) != 2 {
		t.Fatalf("expected 2 replica results, got %#v", payload.Replicas)
	}
	if payload.Replicas[0].NodeID != "node-2" || payload.Replicas[0].State != "ready" {
		t.Fatalf("expected ready replica for node-2, got %#v", payload.Replicas[0])
	}
	if payload.Replicas[1].NodeID != "node-3" || payload.Replicas[1].State != "pending" {
		t.Fatalf("expected pending replica for node-3, got %#v", payload.Replicas[1])
	}
	if got := string(secondaryData["/chunks/chunk-forward"]); got != string(data) {
		t.Fatalf("expected forwarded chunk data %q, got %q", data, got)
	}
}

func TestHTTPHandler_RecordsBusinessMetricsAndStoredChunks(t *testing.T) {
	store, err := NewStore(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	registry := metrics.NewRegistry("datanode")
	handler, err := newHTTPHandler(store, registry, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "http://node-2.local/chunks/chunk-1":
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"chunk":{"chunk_id":"chunk-1"}}`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "http://node-3.local/chunks/chunk-1":
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader([]byte(`boom`))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			t.Fatalf("unexpected forwarded request %s", req.URL.String())
			return nil, nil
		}
	})})
	if err != nil {
		t.Fatalf("new http handler: %v", err)
	}

	data := []byte("chunk-data")
	sum := sha256.Sum256(data)

	putReq := httptest.NewRequest(http.MethodPut, "/chunks/chunk-1", bytes.NewReader(data))
	putReq.Header.Set("X-File-ID", "file-1")
	putReq.Header.Set("X-Checksum-Algorithm", "sha256")
	putReq.Header.Set("X-Checksum-Value", hex.EncodeToString(sum[:]))
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, putReq)
	if putRecorder.Code != http.StatusCreated {
		t.Fatalf("expected 201 from put chunk, got %d", putRecorder.Code)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/chunks/chunk-1", nil)
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200 from get chunk, got %d", getRecorder.Code)
	}

	replicateBody, err := json.Marshal(ReplicateChunkRequest{
		ChunkID: "chunk-1",
		Targets: []ReplicaTarget{
			{NodeID: "node-2", Address: "http://node-2.local"},
			{NodeID: "node-3", Address: "http://node-3.local"},
		},
	})
	if err != nil {
		t.Fatalf("marshal replicate body: %v", err)
	}
	replicateReq := httptest.NewRequest(http.MethodPost, "/internal/replicate", bytes.NewReader(replicateBody))
	replicateReq.Header.Set("Content-Type", "application/json")
	replicateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(replicateRecorder, replicateReq)
	if replicateRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200 from replicate, got %d", replicateRecorder.Code)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/chunks/chunk-1", nil)
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteReq)
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from delete chunk, got %d", deleteRecorder.Code)
	}

	families := scrapeMetricsFamilies(t, registry)

	chunkPut := metricFamilyByName(t, families, "astrastorage_datanode_chunk_put_total")
	assertMetricValue(t, chunkPut.GetMetric(), map[string]string{"result": "success"}, 1)

	chunkGet := metricFamilyByName(t, families, "astrastorage_datanode_chunk_get_total")
	assertMetricValue(t, chunkGet.GetMetric(), map[string]string{"result": "success"}, 1)

	chunkDelete := metricFamilyByName(t, families, "astrastorage_datanode_chunk_delete_total")
	assertMetricValue(t, chunkDelete.GetMetric(), map[string]string{"result": "success"}, 1)

	replicateRequests := metricFamilyByName(t, families, "astrastorage_datanode_replicate_requests_total")
	assertMetricValue(t, replicateRequests.GetMetric(), map[string]string{"result": "degraded"}, 1)

	replicateTargets := metricFamilyByName(t, families, "astrastorage_datanode_replicate_targets_total")
	assertMetricValue(t, replicateTargets.GetMetric(), map[string]string{"result": "success"}, 1)
	assertMetricValue(t, replicateTargets.GetMetric(), map[string]string{"result": "failure"}, 1)

	storedChunks := metricFamilyByName(t, families, "astrastorage_datanode_stored_chunks")
	assertGaugeValue(t, storedChunks.GetMetric(), map[string]string{}, 0)
}

func newDataNodeTestHandler(t *testing.T) (http.Handler, *metrics.Registry, error) {
	t.Helper()
	store, err := NewStore(Config{DataDir: t.TempDir()})
	if err != nil {
		return nil, nil, err
	}
	registry := metrics.NewRegistry("datanode")
	handler, err := NewHTTPHandler(store, registry)
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

func assertGaugeValue(t *testing.T, metrics []*dto.Metric, want map[string]string, value float64) {
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
			if got := metric.GetGauge().GetValue(); got != value {
				t.Fatalf("expected gauge value %v for labels %v, got %v", value, want, got)
			}
			return
		}
	}
	t.Fatalf("gauge with labels %v not found", want)
}

func labelValue(metric *dto.Metric, name string) string {
	for _, label := range metric.GetLabel() {
		if label.GetName() == name {
			return label.GetValue()
		}
	}
	return ""
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
