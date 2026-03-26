# AstraStorage Observability Foundation Design

## Status

- Date: `2026-03-22`
- Scope: observability foundation for `gateway`, `mds`, `datanode`
- Stage: approved design, implementation not started

## Background

`AstraStorage` already has a working MVP data path:

`client -> gateway -> mds -> datanode -> mds -> gateway -> client`

The current gap is not core functionality. The gap is lack of stable observability primitives for:

- understanding request behavior
- debugging upload / download / delete failures
- inspecting repair loop progress
- preparing the codebase for future Redis / RabbitMQ / PostgreSQL cluster integration
- preparing the services for Kubernetes deployment

The repository architecture already reserves `internal/platform/observability` for shared observability infrastructure, but the actual code is not implemented yet.

## Goals

This design defines the first observability foundation that should:

1. provide a minimal but correct monitoring base for current services
2. avoid one-off service-specific instrumentation that will later need refactoring
3. keep application observability separate from infrastructure observability
4. preserve clear extension paths for Redis, RabbitMQ, PostgreSQL, and Kubernetes

## Non-Goals

This phase does not try to ship a complete production monitoring stack.

It explicitly does not include:

- distributed tracing backend
- Grafana dashboards as a required deliverable
- Alertmanager or alert pipelines
- a standalone `monitor` process
- Redis cluster metrics integration
- RabbitMQ cluster metrics integration
- PostgreSQL cluster exporter integration
- Kubernetes monitoring manifests

## Design Principles

### 1. Separate platform primitives from business semantics

`internal/platform/observability` should provide common building blocks:

- metrics registry and handlers
- structured logging helpers
- request / run correlation helpers
- HTTP middleware and client wrappers

It should not define business-specific metrics such as upload success, repair failure, or chunk commit behavior. Those belong to the service packages that own the business flow.

### 2. Keep metrics low-cardinality

Metrics labels must remain stable and bounded.

Allowed dimensions include:

- `service`
- `component`
- `route`
- `method`
- `status_class`
- `result`
- `target`
- `operation`

Disallowed metric labels include:

- `file_id`
- `chunk_id`
- `session_id`
- `inode_id`
- `node_id`
- raw path fragments with unbounded identifiers

Those identifiers belong in logs, not metric labels.

### 3. Use structured logs for high-cardinality context

Logs should carry the identifiers needed for debugging:

- `request_id`
- `run_id`
- `file_id`
- `session_id`
- `chunk_id`
- `node_id`
- `error`
- `error_code`

This keeps the metrics safe while still allowing deep debugging.

### 4. Instrument stable boundaries first

Instrumentation should be attached to stable boundaries that already exist in the codebase:

- service process bootstrap in `cmd/*/app.go`
- inbound HTTP handlers
- outbound HTTP clients
- MDS handler boundary
- background loops such as the pending replica repairer

This avoids coupling metrics to internal implementation churn.

## Architecture Overview

The observability design is split into four layers.

### Layer 1: Application Observability Foundation

Location:

- `internal/platform/observability/metrics`
- `internal/platform/observability/logging`
- `internal/platform/observability/health`

Responsibilities:

- create and own metrics registry wiring
- expose `/metrics`
- provide structured JSON logging
- provide request ID and run ID propagation helpers
- provide HTTP middleware for inbound requests
- provide wrappers or helpers for outbound HTTP observation

This is the only layer to be implemented in the first phase.

### Layer 2: Service Business Observability

Location:

- `internal/gateway`
- `internal/mds`
- `internal/mds/coordinator`
- `internal/datanode`

Responsibilities:

- define service-specific business metrics
- define business event logs
- classify success / failure at business boundaries

Examples:

- gateway upload and download metrics
- MDS RPC method metrics
- repair loop metrics
- datanode chunk and replication metrics

### Layer 3: Dependency Access Observability

Future scope only.

Responsibilities:

- PostgreSQL client call metrics
- Redis client call metrics
- RabbitMQ producer / consumer metrics

This layer describes how AstraStorage interacts with dependencies. It does not monitor the dependency clusters themselves.

### Layer 4: Infrastructure Monitoring

Future scope only.

Responsibilities:

- PostgreSQL cluster exporters and health
- Redis cluster exporters and health
- RabbitMQ cluster exporters and health
- Kubernetes node / pod / workload monitoring

This layer monitors platform state rather than application call behavior.

## Package Plan

### `internal/platform/observability/metrics`

Phase 1 responsibilities:

- registry construction and service metadata
- `/metrics` HTTP handler integration
- inbound HTTP metrics middleware
- basic outbound HTTP call observation helper
- metric naming and route normalization helpers

Suggested internal concepts:

- `Registry` or `ServiceMetrics` root object
- route-aware HTTP middleware
- helper for status-class bucketing
- helper for duration observation

This package should remain thin. It should not become a dumping ground for business counters.

### `internal/platform/observability/logging`

Phase 1 responsibilities:

- build JSON `slog` logger
- inject common attributes such as service and component
- context helpers for attaching and retrieving logger
- request ID extraction and generation helpers
- run ID helper for background loops

Suggested common attributes:

- `service`
- `component`
- `op`
- `request_id`
- `run_id`

### `internal/platform/observability/health`

Phase 1 responsibilities:

- optional helper placement only
- no major behavior changes required in first iteration

Future use:

- split `live`, `ready`, and `startup` health semantics

## Service Instrumentation Plan

### Gateway

Stable boundaries already present:

- bootstrap: `cmd/gateway/app.go`
- inbound HTTP: `internal/gateway/http.go`
- outbound HTTP: `internal/gateway/client.go`

Phase 1 instrumentation:

1. inbound HTTP request metrics and request logs
2. upload / download / delete business counters
3. outbound call metrics for:
   - MDS RPC
   - datanode `PUT /chunks/<id>`
   - datanode `GET /chunks/<id>`
   - datanode `DELETE /chunks/<id>`
   - datanode `POST /internal/replicate`
4. request ID propagation to all outbound requests

### MDS

Stable boundaries already present:

- bootstrap: `cmd/mds/app.go`
- HTTP/RPC entrypoint: `internal/mds/rpc/http.go`
- handler boundary: `internal/mds/handler.go`
- background loop: `internal/mds/coordinator/repairer.go`

Phase 1 instrumentation:

1. inbound HTTP and RPC metrics
2. per-RPC-method counters and latency
3. business counters for upload and node lifecycle operations
4. repair loop counters, duration, and deferred/failed/succeeded totals
5. background run ID per repair cycle

### Datanode

Stable boundaries already present:

- bootstrap: `cmd/datanode/app.go`
- inbound HTTP: `internal/datanode/http.go`

Phase 1 instrumentation:

1. inbound HTTP request metrics and request logs
2. chunk `put/get/delete` counters and latency
3. replication request counters and per-target outcomes
4. node registration and heartbeat success/failure counts

## Metrics Model

The first phase should only expose low-cardinality metrics.

### Shared HTTP Metrics

- `astra_http_requests_total{service,route,method,status_class}`
- `astra_http_request_duration_seconds{service,route,method}`
- `astra_http_in_flight_requests{service,route}`

### Gateway Metrics

- `astra_gateway_upload_requests_total{result}`
- `astra_gateway_upload_chunks_total{result}`
- `astra_gateway_upload_bytes_total`
- `astra_gateway_download_requests_total{result}`
- `astra_gateway_download_bytes_total`
- `astra_gateway_delete_requests_total{result}`
- `astra_gateway_upstream_calls_total{target,operation,result}`
- `astra_gateway_upstream_call_duration_seconds{target,operation}`

### MDS Metrics

- `astra_mds_rpc_requests_total{method,result}`
- `astra_mds_rpc_duration_seconds{method}`
- `astra_mds_upload_sessions_started_total{result}`
- `astra_mds_chunks_committed_total{result}`
- `astra_mds_uploads_completed_total{result}`
- `astra_mds_download_plans_built_total{result}`
- `astra_mds_nodes_registered_total{result}`
- `astra_mds_node_heartbeats_total{result}`
- `astra_mds_allocate_targets_total{result}`

### Repairer Metrics

- `astra_mds_repair_runs_total{result}`
- `astra_mds_repair_run_duration_seconds`
- `astra_mds_repair_replicas_attempted_total`
- `astra_mds_repair_replicas_succeeded_total`
- `astra_mds_repair_replicas_failed_total`
- `astra_mds_repair_targets_deferred_total`

### Datanode Metrics

- `astra_datanode_chunk_put_total{result}`
- `astra_datanode_chunk_get_total{result}`
- `astra_datanode_chunk_delete_total{result}`
- `astra_datanode_replicate_requests_total{result}`
- `astra_datanode_replicate_targets_total{result}`
- `astra_datanode_heartbeats_total{result}`
- `astra_datanode_registered_total{result}`

## Logging Model

Phase 1 logs should be structured JSON using `log/slog`.

### Required Common Fields

- `ts`
- `level`
- `service`
- `component`
- `op`

### Request Correlation Fields

- `request_id`
- `run_id`

### HTTP Request Fields

- `method`
- `path`
- `route`
- `status`
- `duration_ms`
- `remote_addr`

### Business Context Fields

- `file_id`
- `inode_id`
- `session_id`
- `chunk_id`
- `node_id`
- `replica_count`
- `error`
- `error_code`

### Logging Restrictions

Never log:

- `content_base64`
- chunk payload bytes
- full request bodies for data paths
- unbounded raw metadata blobs

## Correlation and Trace Readiness

Phase 1 should introduce request correlation without requiring a tracing backend.

### HTTP Request Correlation

- gateway accepts `X-Request-ID` if present or generates one
- gateway forwards `X-Request-ID` to MDS and datanode
- MDS and datanode extract this header into request logs

### Background Correlation

- repair loop generates a `run_id` per repair cycle
- logs and metrics around the cycle use this run ID in logs only

This gives immediate debugging value and preserves a clean path to future OpenTelemetry adoption.

## Future Extension Plan

### PostgreSQL

Application-side future extension:

- repository call count
- transaction count
- latency
- error classification
- pool metrics

Infrastructure-side future extension:

- cluster exporter
- replication lag
- failover state
- lock and connection pressure

### Redis Cluster

Application-side future extension:

- command count and latency
- timeout / retry behavior
- hit / miss metrics where applicable

Infrastructure-side future extension:

- cluster health
- slot distribution
- replication health
- memory pressure and eviction

### RabbitMQ Cluster

Application-side future extension:

- publish count and latency
- consume count and latency
- ack / nack / retry / dead-letter counts

Infrastructure-side future extension:

- queue depth
- consumer lag
- node status
- connection and channel metrics

### Kubernetes

This design remains valid after moving to Kubernetes because:

- services still expose `/metrics`
- logs still go to stdout as structured JSON
- request correlation still works across pods

Future Kubernetes monitoring should add:

- `kube-state-metrics`
- node and container resource metrics
- scrape configuration for AstraStorage services
- environment fields such as `namespace`, `pod`, `node`, `instance`

## Phased Delivery Plan

### Phase 1: Foundation

- implement shared metrics package
- implement shared logging package
- expose `/metrics` from gateway, MDS, datanode
- add inbound HTTP metrics
- add request ID propagation

### Phase 2: Core Business Instrumentation

- instrument gateway upload/download/delete flows
- instrument MDS RPC methods
- instrument datanode chunk and replicate flows
- instrument repair loop runs and outcomes

### Phase 3: Validation and Documentation

- add tests for middleware behavior and request propagation
- update manual testing steps for `/metrics`
- document expected metrics and log fields
- update technical debt roadmap with scope boundaries and next observability steps

### Phase 4: Future Integration

- add dependency client instrumentation for PostgreSQL, Redis, RabbitMQ
- add dashboards and alerting
- integrate with Kubernetes monitoring stack

## Validation Strategy

Before claiming the observability foundation is complete, validation should cover:

1. unit tests for middleware and helpers
2. request propagation tests for `X-Request-ID`
3. handler-level tests proving `/metrics` is exposed
4. manual verification that upload, repair, download, and delete change the expected counters
5. `go test ./...`
6. `go build ./...`

## Risks and Tradeoffs

### Risk: over-abstracting too early

Mitigation:

- keep platform observability packages small
- define business metrics in service packages

### Risk: metric sprawl and inconsistent naming

Mitigation:

- central naming rules in the metrics package
- use shared result and status conventions

### Risk: high-cardinality labels

Mitigation:

- explicitly ban object IDs as labels
- keep high-cardinality context in logs only

### Risk: mixing application and infrastructure concerns

Mitigation:

- keep exporter and cluster health integration out of phase 1
- treat infrastructure monitoring as a future layer

## Decision Summary

The approved direction is:

- implement a thin shared observability foundation first
- keep business metrics near the services that own the behavior
- instrument current stable boundaries rather than deep internals
- design now for future Redis, RabbitMQ, PostgreSQL, and Kubernetes adoption without implementing those integrations yet
