package gateway

import (
	"errors"
	"fmt"
	"time"

	"AstraStorage/internal/platform/observability/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	gatewayUploadRequestsMetricName   = "astrastorage_gateway_upload_requests_total"
	gatewayUploadChunksMetricName     = "astrastorage_gateway_upload_chunks_total"
	gatewayUploadBytesMetricName      = "astrastorage_gateway_upload_bytes_total"
	gatewayDownloadRequestsMetricName = "astrastorage_gateway_download_requests_total"
	gatewayDownloadBytesMetricName    = "astrastorage_gateway_download_bytes_total"
	gatewayDeleteRequestsMetricName   = "astrastorage_gateway_delete_requests_total"
	gatewayUpstreamRequestsMetricName = "astrastorage_gateway_upstream_requests_total"
	gatewayUpstreamDurationMetricName = "astrastorage_gateway_upstream_request_duration_seconds"
)

type gatewayObservability struct {
	uploadRequests   *prometheus.CounterVec
	uploadChunks     *prometheus.CounterVec
	uploadBytes      prometheus.Counter
	downloadRequests *prometheus.CounterVec
	downloadBytes    prometheus.Counter
	deleteRequests   *prometheus.CounterVec
	upstreamRequests *prometheus.CounterVec
	upstreamDuration *prometheus.HistogramVec
}

func newGatewayObservability(registry *metrics.Registry) (*gatewayObservability, error) {
	obs := &gatewayObservability{
		uploadRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: gatewayUploadRequestsMetricName,
				Help: "Total upload requests handled by the gateway.",
			},
			[]string{"result"},
		),
		uploadChunks: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: gatewayUploadChunksMetricName,
				Help: "Total upload chunks committed by the gateway.",
			},
			[]string{"result"},
		),
		uploadBytes: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: gatewayUploadBytesMetricName,
				Help: "Total upload bytes accepted by the gateway.",
			},
		),
		downloadRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: gatewayDownloadRequestsMetricName,
				Help: "Total download requests handled by the gateway.",
			},
			[]string{"result"},
		),
		downloadBytes: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: gatewayDownloadBytesMetricName,
				Help: "Total bytes returned by successful gateway downloads.",
			},
		),
		deleteRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: gatewayDeleteRequestsMetricName,
				Help: "Total delete requests handled by the gateway.",
			},
			[]string{"result"},
		),
		upstreamRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: gatewayUpstreamRequestsMetricName,
				Help: "Total outbound upstream requests issued by the gateway.",
			},
			[]string{"target", "operation", "result"},
		),
		upstreamDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    gatewayUpstreamDurationMetricName,
				Help:    "Duration of outbound upstream requests issued by the gateway.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"target", "operation", "result"},
		),
	}

	if err := registerGatewayCounterVec(registry, &obs.uploadRequests); err != nil {
		return nil, err
	}
	if err := registerGatewayCounterVec(registry, &obs.uploadChunks); err != nil {
		return nil, err
	}
	if err := registerGatewayCounter(registry, &obs.uploadBytes); err != nil {
		return nil, err
	}
	if err := registerGatewayCounterVec(registry, &obs.downloadRequests); err != nil {
		return nil, err
	}
	if err := registerGatewayCounter(registry, &obs.downloadBytes); err != nil {
		return nil, err
	}
	if err := registerGatewayCounterVec(registry, &obs.deleteRequests); err != nil {
		return nil, err
	}
	if err := registerGatewayCounterVec(registry, &obs.upstreamRequests); err != nil {
		return nil, err
	}
	if err := registerGatewayHistogramVec(registry, &obs.upstreamDuration); err != nil {
		return nil, err
	}
	return obs, nil
}

func registerGatewayCounterVec(registry *metrics.Registry, collector **prometheus.CounterVec) error {
	if err := registry.Register(*collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(*prometheus.CounterVec)
			if !ok {
				return fmt.Errorf("register gateway collector: unexpected existing collector type %T", alreadyRegistered.ExistingCollector)
			}
			*collector = existing
			return nil
		}
		return fmt.Errorf("register gateway collector: %w", err)
	}
	return nil
}

func registerGatewayCounter(registry *metrics.Registry, collector *prometheus.Counter) error {
	if err := registry.Register(*collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(prometheus.Counter)
			if !ok {
				return fmt.Errorf("register gateway collector: unexpected existing collector type %T", alreadyRegistered.ExistingCollector)
			}
			*collector = existing
			return nil
		}
		return fmt.Errorf("register gateway collector: %w", err)
	}
	return nil
}

func registerGatewayHistogramVec(registry *metrics.Registry, collector **prometheus.HistogramVec) error {
	if err := registry.Register(*collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(*prometheus.HistogramVec)
			if !ok {
				return fmt.Errorf("register gateway collector: unexpected existing collector type %T", alreadyRegistered.ExistingCollector)
			}
			*collector = existing
			return nil
		}
		return fmt.Errorf("register gateway collector: %w", err)
	}
	return nil
}

func (o *gatewayObservability) recordUploadRequest(result string) {
	if o == nil {
		return
	}
	o.uploadRequests.WithLabelValues(result).Inc()
}

func (o *gatewayObservability) recordUploadChunk(result string) {
	if o == nil {
		return
	}
	o.uploadChunks.WithLabelValues(result).Inc()
}

func (o *gatewayObservability) recordUploadChunks(result string, count int) {
	if o == nil || count <= 0 {
		return
	}
	for i := 0; i < count; i++ {
		o.uploadChunks.WithLabelValues(result).Inc()
	}
}

func (o *gatewayObservability) recordUploadBytes(bytes int64) {
	if o == nil || bytes <= 0 {
		return
	}
	o.uploadBytes.Add(float64(bytes))
}

func (o *gatewayObservability) recordDownloadRequest(result string) {
	if o == nil {
		return
	}
	o.downloadRequests.WithLabelValues(result).Inc()
}

func (o *gatewayObservability) recordDownloadBytes(bytes int64) {
	if o == nil || bytes <= 0 {
		return
	}
	o.downloadBytes.Add(float64(bytes))
}

func (o *gatewayObservability) recordDeleteRequest(result string) {
	if o == nil {
		return
	}
	o.deleteRequests.WithLabelValues(result).Inc()
}

func (o *gatewayObservability) recordUpstreamRequest(target, operation, result string, duration time.Duration) {
	if o == nil {
		return
	}
	o.upstreamRequests.WithLabelValues(target, operation, result).Inc()
	o.upstreamDuration.WithLabelValues(target, operation, result).Observe(duration.Seconds())
}
