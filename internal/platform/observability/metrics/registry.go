package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	httpRequestsMetricName     = "astrastorage_http_requests_total"
	httpRequestDurationMetric  = "astrastorage_http_request_duration_seconds"
	httpInFlightRequestsMetric = "astrastorage_http_in_flight_requests"
)

type httpMetrics struct {
	RequestsTotal    *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	InFlightRequests *prometheus.GaugeVec
}

type Registry struct {
	service  string
	registry *prometheus.Registry
	metrics  *httpMetrics
}

func NewRegistry(service string) *Registry {
	registry := prometheus.NewRegistry()
	metrics := &httpMetrics{
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: httpRequestsMetricName,
				Help: "Total HTTP requests handled by the service.",
			},
			[]string{"service", "route", "status_class"},
		),
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    httpRequestDurationMetric,
				Help:    "HTTP request duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"service", "route", "status_class"},
		),
		InFlightRequests: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: httpInFlightRequestsMetric,
				Help: "Current in-flight HTTP requests.",
			},
			[]string{"service", "route"},
		),
	}

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		metrics.RequestsTotal,
		metrics.RequestDuration,
		metrics.InFlightRequests,
	)

	return &Registry{
		service:  service,
		registry: registry,
		metrics:  metrics,
	}
}

func (r *Registry) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{})
}

func (r *Registry) Register(collectors ...prometheus.Collector) error {
	for _, collector := range collectors {
		if err := r.registry.Register(collector); err != nil {
			return err
		}
	}
	return nil
}
