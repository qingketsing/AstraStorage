package rpc

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"AstraStorage/internal/platform/observability/logging"
	"AstraStorage/internal/platform/observability/metrics"
	"log/slog"
)

func TestHTTPHandler_ReusesInboundRequestIDAndExposesContext(t *testing.T) {
	var logBuf bytes.Buffer
	oldFactory := requestLoggerFactory
	requestLoggerFactory = func(service, component string) *slog.Logger {
		return logging.NewLogger(&logBuf, service, component)
	}
	t.Cleanup(func() { requestLoggerFactory = oldFactory })

	mux := http.NewServeMux()
	innerSeen := ""
	mux.HandleFunc("/rpc/", func(w http.ResponseWriter, r *http.Request) {
		innerSeen = logging.RequestIDFromContext(r.Context())
		if innerSeen == "" {
			t.Fatalf("expected request id in context")
		}
		w.WriteHeader(http.StatusNoContent)
	})

	handler := &httpHandler{
		registry: metrics.NewRegistry("mds"),
		logger:   requestLoggerFactory("mds", "http"),
		mux:      mux,
	}

	req := httptest.NewRequest(http.MethodPost, "/rpc/"+MethodCreateFile, bytes.NewReader([]byte(`{"bad":`)))
	req.Header.Set("Content-Type", "application/json")
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
	if !strings.Contains(logBuf.String(), "\"route\":\"/rpc/:method\"") {
		t.Fatalf("expected normalized route in log output, got %q", logBuf.String())
	}
}

func TestHTTPHandler_AssignsRequestIDWhenMissing(t *testing.T) {
	var logBuf bytes.Buffer
	oldFactory := requestLoggerFactory
	requestLoggerFactory = func(service, component string) *slog.Logger {
		return logging.NewLogger(&logBuf, service, component)
	}
	t.Cleanup(func() { requestLoggerFactory = oldFactory })

	mux := http.NewServeMux()
	innerSeen := ""
	mux.HandleFunc("/rpc/", func(w http.ResponseWriter, r *http.Request) {
		innerSeen = logging.RequestIDFromContext(r.Context())
		if innerSeen == "" {
			t.Fatalf("expected request id in context")
		}
		logging.SetRequestIDHeader(w.Header(), innerSeen)
		w.WriteHeader(http.StatusNoContent)
	})

	handler := &httpHandler{
		registry: metrics.NewRegistry("mds"),
		logger:   requestLoggerFactory("mds", "http"),
		mux:      mux,
	}

	req := httptest.NewRequest(http.MethodPost, "/rpc/"+MethodCreateFile, bytes.NewReader([]byte(`{"bad":`)))
	req.Header.Set("Content-Type", "application/json")
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
	if !strings.Contains(logBuf.String(), "\"route\":\"/rpc/:method\"") {
		t.Fatalf("expected normalized route in log output, got %q", logBuf.String())
	}
}
