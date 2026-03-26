package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"AstraStorage/internal/datanode"
	mdsrpc "AstraStorage/internal/mds/rpc"
	"AstraStorage/internal/platform/observability/logging"
)

func TestNewApplication_BootstrapsDataNode(t *testing.T) {
	app, err := newApplicationWithConfig(datanode.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if app.store == nil || app.httpServer == nil {
		t.Fatalf("expected store and http server to be initialized")
	}
	if app.httpAddr == "" {
		t.Fatalf("expected http addr to be initialized")
	}
}

func TestNewApplication_HTTPServerServesHealthz(t *testing.T) {
	app, err := newApplicationWithConfig(datanode.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	app.httpServer.Handler.ServeHTTP(recorder, req)
	resp := recorder.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from healthz, got %d", resp.StatusCode)
	}
}

func TestNewApplication_HTTPServerServesMetrics(t *testing.T) {
	app, err := newApplicationWithConfig(datanode.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	app.httpServer.Handler.ServeHTTP(recorder, req)
	resp := recorder.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from metrics, got %d", resp.StatusCode)
	}
	body := new(bytes.Buffer)
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read metrics body: %v", err)
	}
	if !strings.Contains(body.String(), "go_goroutines") {
		t.Fatalf("expected built-in prometheus metric line in body, got %q", body.String())
	}
}

func TestNewApplication_AttachesMDSClientObservability(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "http://mds.local/rpc/mds.register_node" {
			t.Fatalf("unexpected request url %s", r.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"node":{"id":"node-1"}}`)),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldTransport })

	app, err := newApplicationWithConfig(datanode.Config{
		DataDir:        t.TempDir(),
		MDSHTTPBaseURL: "http://mds.local",
		NodeID:         "node-1",
		AdvertiseURL:   "http://127.0.0.1:10080",
		CapacityBytes:  1024,
	})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if app.mdsClient == nil {
		t.Fatalf("expected mds client to be initialized")
	}

	now := time.Now().UTC()
	ctx := logging.WithRequestID(context.Background(), "req-register")
	if err := app.mdsClient.RegisterNode(ctx, datanode.NodeRegistration{
		NodeID:     "node-1",
		Address:    "http://127.0.0.1:10080",
		Capacity:   1024,
		Healthy:    true,
		LastSeenAt: &now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("register node: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	app.httpServer.Handler.ServeHTTP(recorder, req)
	body := recorder.Body.String()

	if !strings.Contains(body, "astrastorage_datanode_upstream_requests_total") {
		t.Fatalf("expected datanode upstream metric family in body, got %q", body)
	}
	if !strings.Contains(body, `operation="mds.register_node"`) {
		t.Fatalf("expected register_node operation label in body, got %q", body)
	}
	if !strings.Contains(body, `result="success"`) {
		t.Fatalf("expected success result label in body, got %q", body)
	}
}

func TestApplication_RegisterNodeSendsRealUsedBytes(t *testing.T) {
	now := time.Now().UTC()
	var captured mdsrpc.RegisterNodeRequest
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/rpc/"+mdsrpc.MethodRegisterNode {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		payload, _ := json.Marshal(mdsrpc.RegisterNodeResponse{})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldTransport })

	app, err := newApplicationWithConfig(datanode.Config{
		DataDir:        t.TempDir(),
		MDSHTTPBaseURL: "http://mds.local",
		NodeID:         "node-1",
		AdvertiseURL:   "http://127.0.0.1:10080",
		CapacityBytes:  1024,
	})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if _, err := app.store.PutChunk(context.Background(), "chunk-1", "file-1", nil, []byte("register-usage"), now); err != nil {
		t.Fatalf("put chunk: %v", err)
	}
	wantUsed, err := app.store.UsageBytes()
	if err != nil {
		t.Fatalf("UsageBytes() error = %v", err)
	}

	if err := app.registerNode(context.Background(), now); err != nil {
		t.Fatalf("registerNode() error = %v", err)
	}
	if captured.Used != wantUsed {
		t.Fatalf("expected used bytes %d, got %d", wantUsed, captured.Used)
	}
}

func TestApplication_HeartbeatNodeSendsRealUsedBytes(t *testing.T) {
	now := time.Now().UTC()
	var captured mdsrpc.HeartbeatNodeRequest
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/rpc/"+mdsrpc.MethodHeartbeatNode {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		payload, _ := json.Marshal(mdsrpc.HeartbeatNodeResponse{})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldTransport })

	app, err := newApplicationWithConfig(datanode.Config{
		DataDir:        t.TempDir(),
		MDSHTTPBaseURL: "http://mds.local",
		NodeID:         "node-1",
		AdvertiseURL:   "http://127.0.0.1:10080",
		CapacityBytes:  2048,
	})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if _, err := app.store.PutChunk(context.Background(), "chunk-2", "file-2", nil, []byte("heartbeat-usage"), now); err != nil {
		t.Fatalf("put chunk: %v", err)
	}
	wantUsed, err := app.store.UsageBytes()
	if err != nil {
		t.Fatalf("UsageBytes() error = %v", err)
	}

	if err := app.sendHeartbeat(context.Background(), now); err != nil {
		t.Fatalf("sendHeartbeat() error = %v", err)
	}
	if captured.Used != wantUsed {
		t.Fatalf("expected used bytes %d, got %d", wantUsed, captured.Used)
	}
}

func TestApplication_RegisterNodeFailsWhenUsageReadFails(t *testing.T) {
	app, err := newApplicationWithConfig(datanode.Config{
		DataDir:        t.TempDir(),
		MDSHTTPBaseURL: "http://mds.local",
		NodeID:         "node-1",
		AdvertiseURL:   "http://127.0.0.1:10080",
		CapacityBytes:  1024,
	})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(app.dataDir, "chunks")); err != nil {
		t.Fatalf("remove chunks dir: %v", err)
	}

	err = app.registerNode(context.Background(), time.Now().UTC())
	if err == nil {
		t.Fatal("expected registerNode() to fail when usage read fails")
	}
	if !strings.Contains(err.Error(), "usage") && !strings.Contains(err.Error(), "chunks dir") {
		t.Fatalf("expected usage read failure, got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
