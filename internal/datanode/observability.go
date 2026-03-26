package datanode

import (
	"errors"
	"fmt"
	"time"

	"AstraStorage/internal/platform/observability/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	datanodeUpstreamRequestsMetricName      = "astrastorage_datanode_upstream_requests_total"
	datanodeUpstreamDurationMetricName      = "astrastorage_datanode_upstream_request_duration_seconds"
	datanodeChunkPutMetricName              = "astrastorage_datanode_chunk_put_total"
	datanodeChunkGetMetricName              = "astrastorage_datanode_chunk_get_total"
	datanodeChunkDeleteMetricName           = "astrastorage_datanode_chunk_delete_total"
	datanodeReplicateRequestsMetricName     = "astrastorage_datanode_replicate_requests_total"
	datanodeReplicateTargetsMetricName      = "astrastorage_datanode_replicate_targets_total"
	datanodeStoredChunksMetricName          = "astrastorage_datanode_stored_chunks"
	datanodeNodesRegisteredMetricName       = "astrastorage_datanode_nodes_registered_total"
	datanodeHeartbeatsMetricName            = "astrastorage_datanode_heartbeats_total"
	datanodeLastRegistrationTimestampMetric = "astrastorage_datanode_last_registration_timestamp_seconds"
	datanodeLastHeartbeatTimestampMetric    = "astrastorage_datanode_last_heartbeat_timestamp_seconds"
	datanodeLifecycleLastStatusMetricName   = "astrastorage_datanode_lifecycle_last_status"
)

type datanodeObservability struct {
	upstreamRequests *prometheus.CounterVec
	upstreamDuration *prometheus.HistogramVec
	chunkPut         *prometheus.CounterVec
	chunkGet         *prometheus.CounterVec
	chunkDelete      *prometheus.CounterVec
	replicateReqs    *prometheus.CounterVec
	replicateTargets *prometheus.CounterVec
	storedChunks     prometheus.Gauge
	registered       *prometheus.CounterVec
	heartbeats       *prometheus.CounterVec
	lastRegisteredAt prometheus.Gauge
	lastHeartbeatAt  prometheus.Gauge
	lifecycleStatus  *prometheus.GaugeVec
}

func newDatanodeObservability(registry *metrics.Registry) (*datanodeObservability, error) {
	obs := &datanodeObservability{
		upstreamRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: datanodeUpstreamRequestsMetricName,
				Help: "Total outbound upstream requests issued by the datanode.",
			},
			[]string{"target", "operation", "result"},
		),
		upstreamDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    datanodeUpstreamDurationMetricName,
				Help:    "Duration of outbound upstream requests issued by the datanode.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"target", "operation", "result"},
		),
		chunkPut: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: datanodeChunkPutMetricName,
				Help: "Total chunk PUT operations handled by the datanode.",
			},
			[]string{"result"},
		),
		chunkGet: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: datanodeChunkGetMetricName,
				Help: "Total chunk GET operations handled by the datanode.",
			},
			[]string{"result"},
		),
		chunkDelete: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: datanodeChunkDeleteMetricName,
				Help: "Total chunk DELETE operations handled by the datanode.",
			},
			[]string{"result"},
		),
		replicateReqs: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: datanodeReplicateRequestsMetricName,
				Help: "Total internal replicate requests handled by the datanode.",
			},
			[]string{"result"},
		),
		replicateTargets: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: datanodeReplicateTargetsMetricName,
				Help: "Total replicate targets attempted by result.",
			},
			[]string{"result"},
		),
		storedChunks: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: datanodeStoredChunksMetricName,
				Help: "Current number of chunks stored by the datanode.",
			},
		),
		registered: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: datanodeNodesRegisteredMetricName,
				Help: "Total node registration attempts issued by the datanode.",
			},
			[]string{"result"},
		),
		heartbeats: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: datanodeHeartbeatsMetricName,
				Help: "Total heartbeat attempts issued by the datanode.",
			},
			[]string{"result"},
		),
		lastRegisteredAt: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: datanodeLastRegistrationTimestampMetric,
				Help: "Unix timestamp of the last successful node registration.",
			},
		),
		lastHeartbeatAt: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: datanodeLastHeartbeatTimestampMetric,
				Help: "Unix timestamp of the last successful heartbeat.",
			},
		),
		lifecycleStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: datanodeLifecycleLastStatusMetricName,
				Help: "One-hot gauge describing the last register/heartbeat outcome.",
			},
			[]string{"operation", "status"},
		),
	}

	if err := registerDatanodeCounterVec(registry, &obs.upstreamRequests); err != nil {
		return nil, err
	}
	if err := registerDatanodeHistogramVec(registry, &obs.upstreamDuration); err != nil {
		return nil, err
	}
	if err := registerDatanodeCounterVec(registry, &obs.chunkPut); err != nil {
		return nil, err
	}
	if err := registerDatanodeCounterVec(registry, &obs.chunkGet); err != nil {
		return nil, err
	}
	if err := registerDatanodeCounterVec(registry, &obs.chunkDelete); err != nil {
		return nil, err
	}
	if err := registerDatanodeCounterVec(registry, &obs.replicateReqs); err != nil {
		return nil, err
	}
	if err := registerDatanodeCounterVec(registry, &obs.replicateTargets); err != nil {
		return nil, err
	}
	if err := registerDatanodeGauge(registry, &obs.storedChunks); err != nil {
		return nil, err
	}
	if err := registerDatanodeCounterVec(registry, &obs.registered); err != nil {
		return nil, err
	}
	if err := registerDatanodeCounterVec(registry, &obs.heartbeats); err != nil {
		return nil, err
	}
	if err := registerDatanodeGauge(registry, &obs.lastRegisteredAt); err != nil {
		return nil, err
	}
	if err := registerDatanodeGauge(registry, &obs.lastHeartbeatAt); err != nil {
		return nil, err
	}
	if err := registerDatanodeGaugeVec(registry, &obs.lifecycleStatus); err != nil {
		return nil, err
	}
	return obs, nil
}

func registerDatanodeCounterVec(registry *metrics.Registry, collector **prometheus.CounterVec) error {
	if err := registry.Register(*collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(*prometheus.CounterVec)
			if !ok {
				return fmt.Errorf("register datanode collector: unexpected existing collector type %T", alreadyRegistered.ExistingCollector)
			}
			*collector = existing
			return nil
		}
		return fmt.Errorf("register datanode collector: %w", err)
	}
	return nil
}

func registerDatanodeHistogramVec(registry *metrics.Registry, collector **prometheus.HistogramVec) error {
	if err := registry.Register(*collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(*prometheus.HistogramVec)
			if !ok {
				return fmt.Errorf("register datanode collector: unexpected existing collector type %T", alreadyRegistered.ExistingCollector)
			}
			*collector = existing
			return nil
		}
		return fmt.Errorf("register datanode collector: %w", err)
	}
	return nil
}

func registerDatanodeGauge(registry *metrics.Registry, collector *prometheus.Gauge) error {
	if err := registry.Register(*collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(prometheus.Gauge)
			if !ok {
				return fmt.Errorf("register datanode collector: unexpected existing collector type %T", alreadyRegistered.ExistingCollector)
			}
			*collector = existing
			return nil
		}
		return fmt.Errorf("register datanode collector: %w", err)
	}
	return nil
}

func registerDatanodeGaugeVec(registry *metrics.Registry, collector **prometheus.GaugeVec) error {
	if err := registry.Register(*collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(*prometheus.GaugeVec)
			if !ok {
				return fmt.Errorf("register datanode collector: unexpected existing collector type %T", alreadyRegistered.ExistingCollector)
			}
			*collector = existing
			return nil
		}
		return fmt.Errorf("register datanode collector: %w", err)
	}
	return nil
}

func (o *datanodeObservability) recordUpstreamRequest(target, operation, result string, duration time.Duration) {
	if o == nil {
		return
	}
	o.upstreamRequests.WithLabelValues(target, operation, result).Inc()
	o.upstreamDuration.WithLabelValues(target, operation, result).Observe(duration.Seconds())
}

func (o *datanodeObservability) recordChunkPut(result string) {
	if o == nil {
		return
	}
	o.chunkPut.WithLabelValues(result).Inc()
}

func (o *datanodeObservability) recordChunkGet(result string) {
	if o == nil {
		return
	}
	o.chunkGet.WithLabelValues(result).Inc()
}

func (o *datanodeObservability) recordChunkDelete(result string) {
	if o == nil {
		return
	}
	o.chunkDelete.WithLabelValues(result).Inc()
}

func (o *datanodeObservability) recordReplicateRequest(result string) {
	if o == nil {
		return
	}
	o.replicateReqs.WithLabelValues(result).Inc()
}

func (o *datanodeObservability) recordReplicateTarget(result string) {
	if o == nil {
		return
	}
	o.replicateTargets.WithLabelValues(result).Inc()
}

func (o *datanodeObservability) setStoredChunks(count int) {
	if o == nil {
		return
	}
	o.storedChunks.Set(float64(count))
}

func (o *datanodeObservability) recordRegistration(result string, at time.Time) {
	if o == nil {
		return
	}
	o.registered.WithLabelValues(result).Inc()
	o.setLifecycleStatus("register", result == "success")
	if result == "success" && !at.IsZero() {
		o.lastRegisteredAt.Set(float64(at.UTC().Unix()))
	}
}

func (o *datanodeObservability) recordHeartbeat(result string, at time.Time) {
	if o == nil {
		return
	}
	o.heartbeats.WithLabelValues(result).Inc()
	o.setLifecycleStatus("heartbeat", result == "success")
	if result == "success" && !at.IsZero() {
		o.lastHeartbeatAt.Set(float64(at.UTC().Unix()))
	}
}

func (o *datanodeObservability) setLifecycleStatus(operation string, success bool) {
	if o == nil {
		return
	}
	if success {
		o.lifecycleStatus.WithLabelValues(operation, "success").Set(1)
		o.lifecycleStatus.WithLabelValues(operation, "failure").Set(0)
		return
	}
	o.lifecycleStatus.WithLabelValues(operation, "success").Set(0)
	o.lifecycleStatus.WithLabelValues(operation, "failure").Set(1)
}
