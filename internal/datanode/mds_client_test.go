package datanode

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"AstraStorage/internal/platform/observability/logging"
	"AstraStorage/internal/platform/observability/metrics"
)

func TestMDSClient_RegisterNodeAndHeartbeat(t *testing.T) {
	var methods []string
	client, err := newMDSClient("http://mds.local", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.URL.Path)
		switch r.URL.Path {
		case "/rpc/mds.register_node", "/rpc/mds.heartbeat_node":
			body, _ := json.Marshal(map[string]any{"node": map[string]any{"id": "node-1"}})
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(body)),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader([]byte(r.URL.Path))),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		}
	})})
	if err != nil {
		t.Fatalf("new mds client: %v", err)
	}

	now := time.Now().UTC()
	if err := client.RegisterNode(context.Background(), NodeRegistration{
		NodeID:     "node-1",
		Address:    "http://127.0.0.1:10080",
		Capacity:   1024,
		Healthy:    true,
		LastSeenAt: &now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("register node: %v", err)
	}
	if err := client.HeartbeatNode(context.Background(), NodeHeartbeat{
		NodeID:     "node-1",
		Healthy:    true,
		Capacity:   1024,
		Used:       128,
		LastSeenAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("heartbeat node: %v", err)
	}

	if len(methods) != 2 || methods[0] != "/rpc/mds.register_node" || methods[1] != "/rpc/mds.heartbeat_node" {
		t.Fatalf("unexpected rpc calls: %#v", methods)
	}
}

func TestMDSClient_ForwardsRequestIDAndRecordsOutboundMetrics(t *testing.T) {
	var seenRequestIDs []string
	client, err := newMDSClient("http://mds.local", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seenRequestIDs = append(seenRequestIDs, r.Header.Get(logging.RequestIDHeader))
		switch r.URL.Path {
		case "/rpc/mds.register_node", "/rpc/mds.heartbeat_node":
			body, _ := json.Marshal(map[string]any{"node": map[string]any{"id": "node-1"}})
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(body)),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader([]byte(r.URL.Path))),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		}
	})})
	if err != nil {
		t.Fatalf("new mds client: %v", err)
	}

	registry := metrics.NewRegistry("datanode")
	if err := client.AttachObservability(registry); err != nil {
		t.Fatalf("attach observability: %v", err)
	}

	now := time.Now().UTC()
	ctx := logging.WithRequestID(context.Background(), "req-datanode")
	if err := client.RegisterNode(ctx, NodeRegistration{
		NodeID:     "node-1",
		Address:    "http://127.0.0.1:10080",
		Capacity:   1024,
		Healthy:    true,
		LastSeenAt: &now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("register node: %v", err)
	}
	if err := client.HeartbeatNode(ctx, NodeHeartbeat{
		NodeID:     "node-1",
		Healthy:    true,
		Capacity:   1024,
		Used:       128,
		LastSeenAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("heartbeat node: %v", err)
	}

	if len(seenRequestIDs) != 2 {
		t.Fatalf("expected 2 outbound request ids, got %d", len(seenRequestIDs))
	}
	for _, requestID := range seenRequestIDs {
		if requestID != "req-datanode" {
			t.Fatalf("expected request id req-datanode, got %q", requestID)
		}
	}

	families := scrapeMetricsFamilies(t, registry)
	upstreamRequests := metricFamilyByName(t, families, "astrastorage_datanode_upstream_requests_total")
	assertMetricValue(t, upstreamRequests.GetMetric(), map[string]string{
		"target":    "mds",
		"operation": "mds.register_node",
		"result":    "success",
	}, 1)
	assertMetricValue(t, upstreamRequests.GetMetric(), map[string]string{
		"target":    "mds",
		"operation": "mds.heartbeat_node",
		"result":    "success",
	}, 1)

	upstreamDuration := metricFamilyByName(t, families, "astrastorage_datanode_upstream_request_duration_seconds")
	assertHistogramCount(t, upstreamDuration.GetMetric(), map[string]string{
		"target":    "mds",
		"operation": "mds.register_node",
		"result":    "success",
	}, 1)
	assertHistogramCount(t, upstreamDuration.GetMetric(), map[string]string{
		"target":    "mds",
		"operation": "mds.heartbeat_node",
		"result":    "success",
	}, 1)

	registered := metricFamilyByName(t, families, "astrastorage_datanode_nodes_registered_total")
	assertMetricValue(t, registered.GetMetric(), map[string]string{"result": "success"}, 1)

	heartbeats := metricFamilyByName(t, families, "astrastorage_datanode_heartbeats_total")
	assertMetricValue(t, heartbeats.GetMetric(), map[string]string{"result": "success"}, 1)

	lastRegistration := metricFamilyByName(t, families, "astrastorage_datanode_last_registration_timestamp_seconds")
	assertGaugeValue(t, lastRegistration.GetMetric(), map[string]string{}, float64(now.Unix()))

	lastHeartbeat := metricFamilyByName(t, families, "astrastorage_datanode_last_heartbeat_timestamp_seconds")
	assertGaugeValue(t, lastHeartbeat.GetMetric(), map[string]string{}, float64(now.Add(time.Minute).Unix()))

	lifecycle := metricFamilyByName(t, families, "astrastorage_datanode_lifecycle_last_status")
	assertGaugeValue(t, lifecycle.GetMetric(), map[string]string{"operation": "register", "status": "success"}, 1)
	assertGaugeValue(t, lifecycle.GetMetric(), map[string]string{"operation": "register", "status": "failure"}, 0)
	assertGaugeValue(t, lifecycle.GetMetric(), map[string]string{"operation": "heartbeat", "status": "success"}, 1)
	assertGaugeValue(t, lifecycle.GetMetric(), map[string]string{"operation": "heartbeat", "status": "failure"}, 0)
}

func TestMDSClient_HeartbeatFailureUpdatesLifecycleStatus(t *testing.T) {
	client, err := newMDSClient("http://mds.local", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(bytes.NewReader([]byte(`boom`))),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})})
	if err != nil {
		t.Fatalf("new mds client: %v", err)
	}

	registry := metrics.NewRegistry("datanode")
	if err := client.AttachObservability(registry); err != nil {
		t.Fatalf("attach observability: %v", err)
	}

	err = client.HeartbeatNode(context.Background(), NodeHeartbeat{
		NodeID:     "node-1",
		Healthy:    true,
		Capacity:   1024,
		Used:       128,
		LastSeenAt: time.Unix(1700001000, 0).UTC(),
	})
	if err == nil {
		t.Fatalf("expected heartbeat to fail")
	}

	families := scrapeMetricsFamilies(t, registry)

	heartbeats := metricFamilyByName(t, families, "astrastorage_datanode_heartbeats_total")
	assertMetricValue(t, heartbeats.GetMetric(), map[string]string{"result": "failure"}, 1)

	lifecycle := metricFamilyByName(t, families, "astrastorage_datanode_lifecycle_last_status")
	assertGaugeValue(t, lifecycle.GetMetric(), map[string]string{"operation": "heartbeat", "status": "success"}, 0)
	assertGaugeValue(t, lifecycle.GetMetric(), map[string]string{"operation": "heartbeat", "status": "failure"}, 1)
}
