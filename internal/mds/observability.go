package mds

import (
	"errors"
	"fmt"
	"time"

	"AstraStorage/internal/mds/store"
	"AstraStorage/internal/platform/observability/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	mdsRPCRequestsMetricName           = "astrastorage_mds_rpc_requests_total"
	mdsRPCDurationMetricName           = "astrastorage_mds_rpc_request_duration_seconds"
	mdsNodesRegisteredMetricName       = "astrastorage_mds_nodes_registered_total"
	mdsNodeHeartbeatsMetricName        = "astrastorage_mds_node_heartbeats_total"
	mdsUploadSessionsStartedMetricName = "astrastorage_mds_upload_sessions_started_total"
	mdsChunksCommittedMetricName       = "astrastorage_mds_chunks_committed_total"
	mdsUploadsCompletedMetricName      = "astrastorage_mds_uploads_completed_total"
	mdsDownloadPlansBuiltMetricName    = "astrastorage_mds_download_plans_built_total"
	mdsAllocateTargetsMetricName       = "astrastorage_mds_allocate_upload_targets_total"
	mdsRepairRunsMetricName            = "astrastorage_mds_repair_runs_total"
	mdsRepairDurationMetricName        = "astrastorage_mds_repair_run_duration_seconds"
	mdsRepairAttemptedMetricName       = "astrastorage_mds_repair_replicas_attempted_total"
	mdsRepairSucceededMetricName       = "astrastorage_mds_repair_replicas_succeeded_total"
	mdsRepairFailedMetricName          = "astrastorage_mds_repair_replicas_failed_total"
	mdsRepairDeferredMetricName        = "astrastorage_mds_repair_targets_deferred_total"
	mdsLeaderTransitionsMetricName     = "astrastorage_mds_leader_transitions_total"
	mdsLeaderIsLeaderMetricName        = "astrastorage_mds_leader_is_leader"
	mdsLeaderTermMetricName            = "astrastorage_mds_leader_term"
	mdsLeaderFailuresMetricName        = "astrastorage_mds_leader_election_failures_total"
)

type Observability struct {
	rpcRequests           *prometheus.CounterVec
	rpcDuration           *prometheus.HistogramVec
	nodesRegistered       *prometheus.CounterVec
	nodeHeartbeats        *prometheus.CounterVec
	uploadSessionsStarted *prometheus.CounterVec
	chunksCommitted       *prometheus.CounterVec
	uploadsCompleted      *prometheus.CounterVec
	downloadPlansBuilt    *prometheus.CounterVec
	allocateTargets       *prometheus.CounterVec
	repairRuns            *prometheus.CounterVec
	repairDuration        *prometheus.HistogramVec
	repairAttempted       prometheus.Counter
	repairSucceeded       prometheus.Counter
	repairFailed          prometheus.Counter
	repairDeferred        prometheus.Counter
	leaderTransitions     *prometheus.CounterVec
	leaderIsLeader        prometheus.Gauge
	leaderTerm            prometheus.Gauge
	leaderFailures        prometheus.Counter
}

func NewObservability(registry *metrics.Registry) (*Observability, error) {
	if registry == nil {
		return nil, errors.New("mds observability: metrics registry is nil")
	}

	obs := &Observability{
		rpcRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: mdsRPCRequestsMetricName,
				Help: "Total MDS RPC requests handled by method and result.",
			},
			[]string{"method", "result"},
		),
		rpcDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    mdsRPCDurationMetricName,
				Help:    "Duration of MDS RPC requests by method and result.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "result"},
		),
		nodesRegistered: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: mdsNodesRegisteredMetricName,
				Help: "Total node registration attempts handled by the MDS.",
			},
			[]string{"result"},
		),
		nodeHeartbeats: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: mdsNodeHeartbeatsMetricName,
				Help: "Total node heartbeats handled by the MDS.",
			},
			[]string{"result"},
		),
		uploadSessionsStarted: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: mdsUploadSessionsStartedMetricName,
				Help: "Total upload sessions started by the MDS.",
			},
			[]string{"result"},
		),
		chunksCommitted: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: mdsChunksCommittedMetricName,
				Help: "Total chunk commits handled by the MDS.",
			},
			[]string{"result"},
		),
		uploadsCompleted: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: mdsUploadsCompletedMetricName,
				Help: "Total upload completion requests handled by the MDS.",
			},
			[]string{"result"},
		),
		downloadPlansBuilt: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: mdsDownloadPlansBuiltMetricName,
				Help: "Total download plans built by the MDS.",
			},
			[]string{"result"},
		),
		allocateTargets: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: mdsAllocateTargetsMetricName,
				Help: "Total upload target allocation requests handled by the MDS.",
			},
			[]string{"result"},
		),
		repairRuns: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: mdsRepairRunsMetricName,
				Help: "Total pending replica repair runs executed by the MDS.",
			},
			[]string{"result"},
		),
		repairDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    mdsRepairDurationMetricName,
				Help:    "Duration of pending replica repair runs.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"result"},
		),
		repairAttempted: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: mdsRepairAttemptedMetricName,
				Help: "Total replica repair attempts issued by the MDS repairer.",
			},
		),
		repairSucceeded: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: mdsRepairSucceededMetricName,
				Help: "Total repaired replicas marked ready by the MDS repairer.",
			},
		),
		repairFailed: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: mdsRepairFailedMetricName,
				Help: "Total replica repair attempts that did not complete successfully.",
			},
		),
		repairDeferred: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: mdsRepairDeferredMetricName,
				Help: "Total repair targets deferred for a later retry.",
			},
		),
		leaderTransitions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: mdsLeaderTransitionsMetricName,
				Help: "Total MDS leader state transitions by result.",
			},
			[]string{"result"},
		),
		leaderIsLeader: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: mdsLeaderIsLeaderMetricName,
				Help: "Whether this MDS instance currently holds leadership.",
			},
		),
		leaderTerm: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: mdsLeaderTermMetricName,
				Help: "Current leadership term for this MDS instance, or 0 when not leader.",
			},
		),
		leaderFailures: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: mdsLeaderFailuresMetricName,
				Help: "Total leader election failures observed by this MDS instance.",
			},
		),
	}

	if err := registerMDSCounterVec(registry, &obs.rpcRequests); err != nil {
		return nil, err
	}
	if err := registerMDSHistogramVec(registry, &obs.rpcDuration); err != nil {
		return nil, err
	}
	if err := registerMDSCounterVec(registry, &obs.nodesRegistered); err != nil {
		return nil, err
	}
	if err := registerMDSCounterVec(registry, &obs.nodeHeartbeats); err != nil {
		return nil, err
	}
	if err := registerMDSCounterVec(registry, &obs.uploadSessionsStarted); err != nil {
		return nil, err
	}
	if err := registerMDSCounterVec(registry, &obs.chunksCommitted); err != nil {
		return nil, err
	}
	if err := registerMDSCounterVec(registry, &obs.uploadsCompleted); err != nil {
		return nil, err
	}
	if err := registerMDSCounterVec(registry, &obs.downloadPlansBuilt); err != nil {
		return nil, err
	}
	if err := registerMDSCounterVec(registry, &obs.allocateTargets); err != nil {
		return nil, err
	}
	if err := registerMDSCounterVec(registry, &obs.repairRuns); err != nil {
		return nil, err
	}
	if err := registerMDSHistogramVec(registry, &obs.repairDuration); err != nil {
		return nil, err
	}
	if err := registerMDSCounter(registry, &obs.repairAttempted); err != nil {
		return nil, err
	}
	if err := registerMDSCounter(registry, &obs.repairSucceeded); err != nil {
		return nil, err
	}
	if err := registerMDSCounter(registry, &obs.repairFailed); err != nil {
		return nil, err
	}
	if err := registerMDSCounter(registry, &obs.repairDeferred); err != nil {
		return nil, err
	}
	if err := registerMDSCounterVec(registry, &obs.leaderTransitions); err != nil {
		return nil, err
	}
	if err := registerMDSGauge(registry, &obs.leaderIsLeader); err != nil {
		return nil, err
	}
	if err := registerMDSGauge(registry, &obs.leaderTerm); err != nil {
		return nil, err
	}
	if err := registerMDSCounter(registry, &obs.leaderFailures); err != nil {
		return nil, err
	}
	return obs, nil
}

func registerMDSCounterVec(registry *metrics.Registry, collector **prometheus.CounterVec) error {
	if err := registry.Register(*collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(*prometheus.CounterVec)
			if !ok {
				return fmt.Errorf("register mds collector: unexpected existing collector type %T", alreadyRegistered.ExistingCollector)
			}
			*collector = existing
			return nil
		}
		return fmt.Errorf("register mds collector: %w", err)
	}
	return nil
}

func registerMDSHistogramVec(registry *metrics.Registry, collector **prometheus.HistogramVec) error {
	if err := registry.Register(*collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(*prometheus.HistogramVec)
			if !ok {
				return fmt.Errorf("register mds collector: unexpected existing collector type %T", alreadyRegistered.ExistingCollector)
			}
			*collector = existing
			return nil
		}
		return fmt.Errorf("register mds collector: %w", err)
	}
	return nil
}

func registerMDSCounter(registry *metrics.Registry, collector *prometheus.Counter) error {
	if err := registry.Register(*collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(prometheus.Counter)
			if !ok {
				return fmt.Errorf("register mds collector: unexpected existing collector type %T", alreadyRegistered.ExistingCollector)
			}
			*collector = existing
			return nil
		}
		return fmt.Errorf("register mds collector: %w", err)
	}
	return nil
}

func registerMDSGauge(registry *metrics.Registry, collector *prometheus.Gauge) error {
	if err := registry.Register(*collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(prometheus.Gauge)
			if !ok {
				return fmt.Errorf("register mds collector: unexpected existing collector type %T", alreadyRegistered.ExistingCollector)
			}
			*collector = existing
			return nil
		}
		return fmt.Errorf("register mds collector: %w", err)
	}
	return nil
}

func ClassifyResult(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, store.ErrInvalidArgument):
		return "invalid_argument"
	case errors.Is(err, store.ErrNotFound):
		return "not_found"
	case errors.Is(err, store.ErrAlreadyExists):
		return "already_exists"
	case errors.Is(err, store.ErrConflict):
		return "conflict"
	default:
		return "internal"
	}
}

func (o *Observability) RecordRPCRequest(method, result string, duration time.Duration) {
	if o == nil {
		return
	}
	o.rpcRequests.WithLabelValues(method, result).Inc()
	o.rpcDuration.WithLabelValues(method, result).Observe(duration.Seconds())
}

func (o *Observability) RecordRegisterNode(result string) {
	if o == nil {
		return
	}
	o.nodesRegistered.WithLabelValues(result).Inc()
}

func (o *Observability) RecordHeartbeatNode(result string) {
	if o == nil {
		return
	}
	o.nodeHeartbeats.WithLabelValues(result).Inc()
}

func (o *Observability) RecordStartUpload(result string) {
	if o == nil {
		return
	}
	o.uploadSessionsStarted.WithLabelValues(result).Inc()
}

func (o *Observability) RecordCommitChunk(result string) {
	if o == nil {
		return
	}
	o.chunksCommitted.WithLabelValues(result).Inc()
}

func (o *Observability) RecordCompleteUpload(result string) {
	if o == nil {
		return
	}
	o.uploadsCompleted.WithLabelValues(result).Inc()
}

func (o *Observability) RecordBuildDownloadPlan(result string) {
	if o == nil {
		return
	}
	o.downloadPlansBuilt.WithLabelValues(result).Inc()
}

func (o *Observability) RecordAllocateUploadTargets(result string) {
	if o == nil {
		return
	}
	o.allocateTargets.WithLabelValues(result).Inc()
}

func (o *Observability) RecordRepairRun(result string, duration time.Duration) {
	if o == nil {
		return
	}
	o.repairRuns.WithLabelValues(result).Inc()
	o.repairDuration.WithLabelValues(result).Observe(duration.Seconds())
}

func (o *Observability) RecordRepairReplicasAttempted(count int) {
	if o == nil || count <= 0 {
		return
	}
	o.repairAttempted.Add(float64(count))
}

func (o *Observability) RecordRepairReplicasSucceeded(count int) {
	if o == nil || count <= 0 {
		return
	}
	o.repairSucceeded.Add(float64(count))
}

func (o *Observability) RecordRepairReplicasFailed(count int) {
	if o == nil || count <= 0 {
		return
	}
	o.repairFailed.Add(float64(count))
}

func (o *Observability) RecordRepairTargetsDeferred(count int) {
	if o == nil || count <= 0 {
		return
	}
	o.repairDeferred.Add(float64(count))
}

func (o *Observability) RecordLeaderTransition(result string) {
	if o == nil {
		return
	}
	o.leaderTransitions.WithLabelValues(result).Inc()
}

func (o *Observability) SetLeaderState(active bool, term int64) {
	if o == nil {
		return
	}
	if active {
		o.leaderIsLeader.Set(1)
		o.leaderTerm.Set(float64(term))
		return
	}
	o.leaderIsLeader.Set(0)
	o.leaderTerm.Set(0)
}

func (o *Observability) RecordLeaderElectionFailure() {
	if o == nil {
		return
	}
	o.leaderFailures.Inc()
}
