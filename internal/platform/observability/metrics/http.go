package metrics

import (
	"net/http"

	"github.com/felixge/httpsnoop"
)

func (r *Registry) Middleware(service, route string, next http.Handler) http.Handler {
	if service == "" {
		service = r.service
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.metrics.InFlightRequests.WithLabelValues(service, route).Inc()
		defer r.metrics.InFlightRequests.WithLabelValues(service, route).Dec()

		metrics := httpsnoop.CaptureMetricsFn(w, func(ww http.ResponseWriter) {
			next.ServeHTTP(ww, req)
		})
		class := statusClass(metrics.Code)
		r.metrics.RequestsTotal.WithLabelValues(service, route, class).Inc()
		r.metrics.RequestDuration.WithLabelValues(service, route, class).Observe(metrics.Duration.Seconds())
	})
}

func statusClass(status int) string {
	switch {
	// Task 1 keeps only three buckets: treat informational and redirect
	// responses as successful traffic rather than errors.
	case status >= 100 && status < 400:
		return "2xx"
	case status >= 400 && status < 500:
		return "4xx"
	default:
		return "5xx"
	}
}
