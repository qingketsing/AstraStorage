package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestLogger_JSONIncludesServiceField(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "mds", "observability")

	logger.Info("hello world")

	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &payload); err != nil {
		t.Fatalf("unmarshal log line: %v", err)
	}
	if payload["service"] != "mds" {
		t.Fatalf("expected service field, got %#v", payload)
	}
	if payload["component"] != "observability" {
		t.Fatalf("expected component field, got %#v", payload)
	}
	if payload["msg"] != "hello world" {
		t.Fatalf("expected message field, got %#v", payload)
	}
}

func TestRequestIDContext_RoundTrip(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req-123")
	if got := RequestIDFromContext(ctx); got != "req-123" {
		t.Fatalf("expected request id from context, got %q", got)
	}

	header := make(http.Header)
	SetRequestIDHeader(header, "req-456")
	if got := RequestIDFromHeader(header); got != "req-456" {
		t.Fatalf("expected request id from header, got %q", got)
	}
}
