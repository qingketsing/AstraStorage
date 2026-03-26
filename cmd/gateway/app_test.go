package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"AstraStorage/internal/gateway"
)

func TestNewApplication_BootstrapsGateway(t *testing.T) {
	app, err := newApplicationWithConfig(gateway.Config{
		MDSHTTPBaseURL:  "http://mds.local",
		DataNodeBaseURL: "http://datanode.local",
	})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if app.client == nil || app.httpServer == nil {
		t.Fatalf("expected client and http server to be initialized")
	}
	if app.httpAddr == "" {
		t.Fatalf("expected http addr to be initialized")
	}
}

func TestNewApplication_HTTPServerServesHealthz(t *testing.T) {
	app, err := newApplicationWithConfig(gateway.Config{
		MDSHTTPBaseURL:  "http://mds.invalid",
		DataNodeBaseURL: "http://datanode.invalid",
	})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	app.httpServer.Handler.ServeHTTP(recorder, req)
	resp := recorder.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 from healthz without upstreams, got %d", resp.StatusCode)
	}
}

func TestNewApplication_HTTPServerServesMetrics(t *testing.T) {
	app, err := newApplicationWithConfig(gateway.Config{
		MDSHTTPBaseURL:  "http://mds.invalid",
		DataNodeBaseURL: "http://datanode.invalid",
	})
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
