package gateway

import (
	"testing"
	"time"

	"AstraStorage/internal/platform/observability/metrics"
)

func TestGatewayObservability_ReusesCollectorsAcrossHandlers(t *testing.T) {
	registry := metrics.NewRegistry("gateway")

	first, err := newGatewayObservability(registry)
	if err != nil {
		t.Fatalf("new first observability: %v", err)
	}
	second, err := newGatewayObservability(registry)
	if err != nil {
		t.Fatalf("new second observability: %v", err)
	}

	second.recordUploadRequest("success")
	second.recordUploadChunk("success")
	second.recordUploadBytes(7)
	second.recordDownloadRequest("success")
	second.recordDownloadBytes(11)
	second.recordDeleteRequest("success")

	_ = first

	families := scrapeMetricsFamilies(t, registry)

	uploadRequests := metricFamilyByName(t, families, "astrastorage_gateway_upload_requests_total")
	assertMetricValue(t, uploadRequests.GetMetric(), map[string]string{"result": "success"}, 1)

	uploadChunks := metricFamilyByName(t, families, "astrastorage_gateway_upload_chunks_total")
	assertMetricValue(t, uploadChunks.GetMetric(), map[string]string{"result": "success"}, 1)

	uploadBytes := metricFamilyByName(t, families, "astrastorage_gateway_upload_bytes_total")
	assertMetricValue(t, uploadBytes.GetMetric(), map[string]string{}, 7)

	downloadRequests := metricFamilyByName(t, families, "astrastorage_gateway_download_requests_total")
	assertMetricValue(t, downloadRequests.GetMetric(), map[string]string{"result": "success"}, 1)

	downloadBytes := metricFamilyByName(t, families, "astrastorage_gateway_download_bytes_total")
	assertMetricValue(t, downloadBytes.GetMetric(), map[string]string{}, 11)

	deleteRequests := metricFamilyByName(t, families, "astrastorage_gateway_delete_requests_total")
	assertMetricValue(t, deleteRequests.GetMetric(), map[string]string{"result": "success"}, 1)
}

func TestGatewayObservability_ReusesOutboundCollectorsAcrossHandlers(t *testing.T) {
	registry := metrics.NewRegistry("gateway")

	first, err := newGatewayObservability(registry)
	if err != nil {
		t.Fatalf("new first observability: %v", err)
	}
	second, err := newGatewayObservability(registry)
	if err != nil {
		t.Fatalf("new second observability: %v", err)
	}

	second.recordUpstreamRequest("mds", "mds.start_upload", "success", 25*time.Millisecond)

	_ = first

	families := scrapeMetricsFamilies(t, registry)

	upstreamRequests := metricFamilyByName(t, families, "astrastorage_gateway_upstream_requests_total")
	assertMetricValue(t, upstreamRequests.GetMetric(), map[string]string{
		"target":    "mds",
		"operation": "mds.start_upload",
		"result":    "success",
	}, 1)

	upstreamDuration := metricFamilyByName(t, families, "astrastorage_gateway_upstream_request_duration_seconds")
	assertHistogramCount(t, upstreamDuration.GetMetric(), map[string]string{
		"target":    "mds",
		"operation": "mds.start_upload",
		"result":    "success",
	}, 1)
}
