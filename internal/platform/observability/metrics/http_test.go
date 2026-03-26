package metrics

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func TestMetricsHandler_ExposesRegisteredCollectors(t *testing.T) {
	reg := NewRegistry("mds")
	reg.metrics.RequestsTotal.WithLabelValues("mds", "/files/:id", "2xx").Inc()
	reg.metrics.RequestDuration.WithLabelValues("mds", "/files/:id", "2xx").Observe(0.25)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	reg.MetricsHandler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 from metrics handler, got %d", recorder.Code)
	}
	contentType := recorder.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/plain; version=0.0.4") {
		t.Fatalf("expected Prometheus text content type, got %q", contentType)
	}

	parser := expfmt.TextParser{}
	families, err := parser.TextToMetricFamilies(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatalf("parse Prometheus text exposition: %v", err)
	}

	requests := families["astrastorage_http_requests_total"]
	if requests == nil {
		t.Fatalf("expected request counter in metrics output")
	}
	duration := families["astrastorage_http_request_duration_seconds"]
	if duration == nil {
		t.Fatalf("expected request duration histogram in metrics output")
	}

	assertMetricLabels(t, requests.GetMetric(), map[string]string{
		"service":      "mds",
		"route":        "/files/:id",
		"status_class": "2xx",
	})
	assertMetricLabels(t, duration.GetMetric(), map[string]string{
		"service":      "mds",
		"route":        "/files/:id",
		"status_class": "2xx",
	})
}

func TestRegistry_Register_ExposesCustomCollectors(t *testing.T) {
	reg := NewRegistry("mds")
	custom := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "astrastorage_test_custom_total",
		Help: "Custom test collector.",
	})
	if err := reg.Register(custom); err != nil {
		t.Fatalf("register custom collector: %v", err)
	}
	custom.Inc()

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	reg.MetricsHandler().ServeHTTP(recorder, req)

	if !strings.Contains(recorder.Body.String(), "astrastorage_test_custom_total") {
		t.Fatalf("expected custom collector to be exposed, got %q", recorder.Body.String())
	}
}

func TestHTTPMiddleware_RecordsRouteMetrics(t *testing.T) {
	reg := NewRegistry("mds")
	handler := reg.Middleware("mds", "/files/:id", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMovedPermanently)
		_, _ = w.Write([]byte("moved"))
	}))

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/123", nil)
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301 from middleware, got %d", recorder.Code)
	}

	families, err := reg.registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	requests := findMetricFamily(t, families, "astrastorage_http_requests_total")
	assertSampleCount(t, requests, "mds", "/files/:id", "2xx", 1)
	assertCounterValue(t, requests, "mds", "/files/:id", "2xx", 1)

	duration := findMetricFamily(t, families, "astrastorage_http_request_duration_seconds")
	assertSampleCount(t, duration, "mds", "/files/:id", "2xx", 1)
}

func TestHTTPMiddleware_PreservesSupportedResponseWriterInterfaces(t *testing.T) {
	reg := NewRegistry("mds")
	handler := reg.Middleware("mds", "/files/:id", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := any(w).(http.Flusher); !ok {
			t.Fatalf("expected wrapped writer to preserve http.Flusher")
		}
		if _, ok := any(w).(http.Hijacker); ok {
			t.Fatalf("did not expect http.Hijacker on flusher-only writer")
		}
		if _, ok := any(w).(http.Pusher); ok {
			t.Fatalf("did not expect http.Pusher on flusher-only writer")
		}
		w.WriteHeader(http.StatusOK)
	}))

	recorder := &flusherResponseWriter{header: make(http.Header)}
	req := httptest.NewRequest(http.MethodGet, "/files/123", nil)
	handler.ServeHTTP(recorder, req)

	if recorder.code != http.StatusOK {
		t.Fatalf("expected 200 from middleware, got %d", recorder.code)
	}
}

func TestHTTPMiddleware_DoesNotAdvertiseUnsupportedResponseWriterInterfaces(t *testing.T) {
	reg := NewRegistry("mds")
	handler := reg.Middleware("mds", "/files/:id", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := any(w).(http.Flusher); ok {
			t.Fatalf("did not expect http.Flusher on plain writer")
		}
		if _, ok := any(w).(http.Hijacker); ok {
			t.Fatalf("did not expect http.Hijacker on plain writer")
		}
		if _, ok := any(w).(http.Pusher); ok {
			t.Fatalf("did not expect http.Pusher on plain writer")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := &plainResponseWriter{header: make(http.Header)}
	req := httptest.NewRequest(http.MethodGet, "/files/123", nil)
	handler.ServeHTTP(recorder, req)

	if recorder.code != http.StatusNoContent {
		t.Fatalf("expected 204 from middleware, got %d", recorder.code)
	}
}

func TestStatusClass_CategorizesHTTPStatuses(t *testing.T) {
	cases := map[int]string{
		0:   "5xx",
		101: "2xx",
		204: "2xx",
		301: "2xx",
		404: "4xx",
		503: "5xx",
	}

	for status, want := range cases {
		if got := statusClass(status); got != want {
			t.Fatalf("statusClass(%d) = %q, want %q", status, got, want)
		}
	}
}

func assertSampleCount(t *testing.T, family *dto.MetricFamily, service, route, statusClass string, want uint64) {
	t.Helper()
	metric := findMetric(t, family, service, route, statusClass)
	got := metric.GetHistogram().GetSampleCount()
	if family.GetType() == dto.MetricType_COUNTER {
		got = uint64(metric.GetCounter().GetValue())
	}
	if got != want {
		t.Fatalf("unexpected sample count for %s: got %d want %d", family.GetName(), got, want)
	}
}

func assertCounterValue(t *testing.T, family *dto.MetricFamily, service, route, statusClass string, want float64) {
	t.Helper()
	metric := findMetric(t, family, service, route, statusClass)
	got := metric.GetCounter().GetValue()
	if got != want {
		t.Fatalf("unexpected counter value for %s: got %v want %v", family.GetName(), got, want)
	}
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

func findMetric(t *testing.T, family *dto.MetricFamily, service, route, statusClass string) *dto.Metric {
	t.Helper()
	for _, metric := range family.GetMetric() {
		if labelValue(metric, "service") == service && labelValue(metric, "route") == route && labelValue(metric, "status_class") == statusClass {
			return metric
		}
	}
	t.Fatalf("metric with labels service=%q route=%q status_class=%q not found in %s", service, route, statusClass, family.GetName())
	return nil
}

func findMetricFamily(t *testing.T, families []*dto.MetricFamily, name string) *dto.MetricFamily {
	t.Helper()
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}
	t.Fatalf("metric family %s not found", name)
	return nil
}

func labelValue(metric *dto.Metric, name string) string {
	for _, label := range metric.GetLabel() {
		if label.GetName() == name {
			return label.GetValue()
		}
	}
	return ""
}

type flusherResponseWriter struct {
	header http.Header
	code   int
}

func (w *flusherResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *flusherResponseWriter) WriteHeader(status int) {
	w.code = status
}

func (w *flusherResponseWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (w *flusherResponseWriter) Flush() {}

type plainResponseWriter struct {
	header http.Header
	code   int
}

func (w *plainResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *plainResponseWriter) WriteHeader(status int) {
	w.code = status
}

func (w *plainResponseWriter) Write(p []byte) (int, error) {
	return len(p), nil
}
