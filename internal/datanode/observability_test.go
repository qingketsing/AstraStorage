package datanode

import (
	"testing"
	"time"

	"AstraStorage/internal/platform/observability/metrics"
	dto "github.com/prometheus/client_model/go"
)

func TestDatanodeObservability_ReusesCollectorsAcrossClients(t *testing.T) {
	registry := metrics.NewRegistry("datanode")

	first, err := newDatanodeObservability(registry)
	if err != nil {
		t.Fatalf("new first observability: %v", err)
	}
	second, err := newDatanodeObservability(registry)
	if err != nil {
		t.Fatalf("new second observability: %v", err)
	}

	second.recordUpstreamRequest("mds", "mds.register_node", "success", 25*time.Millisecond)
	second.recordChunkPut("success")
	second.recordChunkGet("success")
	second.recordChunkDelete("success")
	second.recordReplicateRequest("degraded")
	second.recordReplicateTarget("success")
	second.recordReplicateTarget("failure")
	second.setStoredChunks(3)
	second.recordRegistration("success", time.Unix(1700000000, 0).UTC())
	second.recordHeartbeat("success", time.Unix(1700000060, 0).UTC())

	_ = first

	families := scrapeMetricsFamilies(t, registry)
	upstreamRequests := metricFamilyByName(t, families, "astrastorage_datanode_upstream_requests_total")
	assertMetricValue(t, upstreamRequests.GetMetric(), map[string]string{
		"target":    "mds",
		"operation": "mds.register_node",
		"result":    "success",
	}, 1)

	upstreamDuration := metricFamilyByName(t, families, "astrastorage_datanode_upstream_request_duration_seconds")
	assertHistogramCount(t, upstreamDuration.GetMetric(), map[string]string{
		"target":    "mds",
		"operation": "mds.register_node",
		"result":    "success",
	}, 1)

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
	assertGaugeValue(t, storedChunks.GetMetric(), map[string]string{}, 3)

	registered := metricFamilyByName(t, families, "astrastorage_datanode_nodes_registered_total")
	assertMetricValue(t, registered.GetMetric(), map[string]string{"result": "success"}, 1)

	heartbeats := metricFamilyByName(t, families, "astrastorage_datanode_heartbeats_total")
	assertMetricValue(t, heartbeats.GetMetric(), map[string]string{"result": "success"}, 1)

	lastRegistration := metricFamilyByName(t, families, "astrastorage_datanode_last_registration_timestamp_seconds")
	assertGaugeValue(t, lastRegistration.GetMetric(), map[string]string{}, 1700000000)

	lastHeartbeat := metricFamilyByName(t, families, "astrastorage_datanode_last_heartbeat_timestamp_seconds")
	assertGaugeValue(t, lastHeartbeat.GetMetric(), map[string]string{}, 1700000060)

	lifecycle := metricFamilyByName(t, families, "astrastorage_datanode_lifecycle_last_status")
	assertGaugeValue(t, lifecycle.GetMetric(), map[string]string{"operation": "register", "status": "success"}, 1)
	assertGaugeValue(t, lifecycle.GetMetric(), map[string]string{"operation": "register", "status": "failure"}, 0)
	assertGaugeValue(t, lifecycle.GetMetric(), map[string]string{"operation": "heartbeat", "status": "success"}, 1)
	assertGaugeValue(t, lifecycle.GetMetric(), map[string]string{"operation": "heartbeat", "status": "failure"}, 0)
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
