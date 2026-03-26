# Observability Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first observability foundation for `gateway`, `mds`, and `datanode` with shared metrics/logging primitives, `/metrics` exposure, request correlation, and core business instrumentation.

**Architecture:** Add a thin shared observability foundation under `internal/platform/observability`, then instrument stable boundaries already present in the codebase: service bootstrap, inbound HTTP handlers, outbound HTTP clients, and the MDS repair loop. Keep shared packages generic and keep business metrics near the services that own the behavior.

**Tech Stack:** Go 1.25, `net/http`, `log/slog`, Prometheus Go client, existing package-level tests with `testing` and `httptest`

---

## File Map

### New files

- `internal/platform/observability/metrics/registry.go`
- `internal/platform/observability/metrics/http.go`
- `internal/platform/observability/metrics/http_test.go`
- `internal/platform/observability/logging/logger.go`
- `internal/platform/observability/logging/context.go`
- `internal/platform/observability/logging/logger_test.go`
- `internal/gateway/observability.go`
- `internal/gateway/observability_test.go`
- `internal/mds/observability.go`
- `internal/mds/observability_test.go`
- `internal/datanode/observability.go`
- `internal/datanode/observability_test.go`

### Modified files

- `go.mod`
- `cmd/gateway/app.go`
- `cmd/gateway/app_test.go`
- `cmd/datanode/app.go`
- `cmd/datanode/app_test.go`
- `cmd/mds/app.go`
- `cmd/mds/app_test.go`
- `internal/gateway/http.go`
- `internal/gateway/http_test.go`
- `internal/gateway/client.go`
- `internal/mds/rpc/http.go`
- `internal/mds/rpc/http_test.go`
- `internal/mds/handler.go`
- `internal/mds/coordinator/repairer.go`
- `internal/mds/coordinator/repairer_test.go`
- `internal/datanode/http.go`
- `internal/datanode/http_test.go`
- `internal/datanode/mds_client.go`
- `internal/datanode/mds_client_test.go`
- `docs/architecture/manual-testing.md`
- `docs/architecture/technical-debt-roadmap.md`

### Responsibility split

- `internal/platform/observability/metrics/*`: shared registry, `/metrics` handler, HTTP metrics middleware, request ID helpers safe for any service
- `internal/platform/observability/logging/*`: shared `slog` setup and context helpers
- `internal/gateway/observability.go`: gateway-specific counters, histograms, and helper methods
- `internal/mds/observability.go`: MDS RPC and node lifecycle metrics
- `internal/datanode/observability.go`: datanode chunk/replication/heartbeat metrics
- existing service files: call the new helper methods at stable boundaries without moving core business logic

## Global Constraints

- Keep all metric labels low-cardinality.
- Never add `file_id`, `chunk_id`, `session_id`, `inode_id`, or `node_id` as metric labels.
- Put high-cardinality identifiers only in logs.
- Use `log/slog` JSON output.
- Propagate `X-Request-ID` from gateway to downstream HTTP requests.
- Add `/metrics` alongside existing `/healthz` handlers.
- Keep shared observability packages thin; do not move business logic into them.

## Task 1: Add Shared Observability Packages

**Files:**
- Modify: `go.mod`
- Create: `internal/platform/observability/metrics/registry.go`
- Create: `internal/platform/observability/metrics/http.go`
- Create: `internal/platform/observability/metrics/http_test.go`
- Create: `internal/platform/observability/logging/logger.go`
- Create: `internal/platform/observability/logging/context.go`
- Create: `internal/platform/observability/logging/logger_test.go`

- [ ] **Step 1: Write failing tests for shared metrics and logging primitives**

Add tests that prove:

- a registry can expose Prometheus text format through an HTTP handler
- HTTP middleware records request count and duration with route labels
- logger creation uses JSON output and includes fixed service/component fields
- request ID helpers round-trip through `context.Context`

Suggested test skeletons:

```go
func TestMetricsHandler_ExposesRegisteredCollectors(t *testing.T) {}

func TestHTTPMiddleware_RecordsRouteMetrics(t *testing.T) {}

func TestLogger_JSONIncludesServiceField(t *testing.T) {}

func TestRequestIDContext_RoundTrip(t *testing.T) {}
```

- [ ] **Step 2: Run the package tests and verify they fail**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/platform/observability/... -v
```

Expected:

- FAIL because the new packages/files do not exist yet

- [ ] **Step 3: Add the minimal shared observability implementation**

Implementation requirements:

- add Prometheus client dependency to `go.mod`
- create a small metrics root object that owns a registry and can return `http.Handler` for `/metrics`
- create HTTP middleware that accepts `service` and normalized `route`
- classify response status into `2xx`, `4xx`, `5xx`
- add logging helpers around `slog.NewJSONHandler`
- add `request_id` getter/setter helpers for context and HTTP headers

Suggested implementation shape:

```go
type HTTPMetrics struct {
    RequestsTotal    *prometheus.CounterVec
    RequestDuration  *prometheus.HistogramVec
    InFlightRequests *prometheus.GaugeVec
}

func NewRegistry(service string) *Registry
func (r *Registry) MetricsHandler() http.Handler
func (r *Registry) Middleware(route string, next http.Handler) http.Handler

func NewLogger(w io.Writer, service string) *slog.Logger
func WithRequestID(ctx context.Context, requestID string) context.Context
func RequestIDFromContext(ctx context.Context) string
```

- [ ] **Step 4: Run the shared package tests and verify they pass**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/platform/observability/... -v
```

Expected:

- PASS for all new metrics/logging tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add go.mod internal/platform/observability
git commit -m "observability: add shared metrics and logging foundation"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 2: Wire `/metrics` Into All Three Services

**Files:**
- Modify: `cmd/gateway/app.go`
- Modify: `cmd/gateway/app_test.go`
- Modify: `cmd/datanode/app.go`
- Modify: `cmd/datanode/app_test.go`
- Modify: `cmd/mds/app.go`
- Modify: `cmd/mds/app_test.go`
- Modify: `internal/gateway/http.go`
- Modify: `internal/datanode/http.go`
- Modify: `internal/mds/rpc/http.go`

- [ ] **Step 1: Write failing app tests that expect `/metrics`**

Add one test per service that calls `/metrics` on the assembled server and checks:

- HTTP status is `200`
- response body contains at least one Prometheus metric line such as `astra_http_requests_total`

Suggested test names:

```go
func TestNewApplication_HTTPServerServesMetrics(t *testing.T) {}
```

- [ ] **Step 2: Run the three app test packages and verify the new tests fail**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./cmd/gateway ./cmd/datanode ./cmd/mds -v
```

Expected:

- FAIL because `/metrics` is not routed yet

- [ ] **Step 3: Wire registry creation and `/metrics` routes**

Implementation requirements:

- construct one shared metrics registry per service at bootstrap
- attach `/metrics` to the existing `ServeMux` for gateway, datanode, and MDS HTTP entrypoints
- keep `/healthz` behavior unchanged

Suggested implementation shape:

```go
mux.Handle("/metrics", registry.MetricsHandler())
```

- [ ] **Step 4: Re-run the app tests**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./cmd/gateway ./cmd/datanode ./cmd/mds -v
```

Expected:

- PASS for new `/metrics` tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add cmd/gateway/app.go cmd/gateway/app_test.go cmd/datanode/app.go cmd/datanode/app_test.go cmd/mds/app.go cmd/mds/app_test.go internal/gateway/http.go internal/datanode/http.go internal/mds/rpc/http.go
git commit -m "observability: expose metrics endpoints"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 3: Add HTTP Middleware and Request Logging

**Files:**
- Modify: `internal/gateway/http.go`
- Modify: `internal/gateway/http_test.go`
- Modify: `internal/datanode/http.go`
- Modify: `internal/datanode/http_test.go`
- Modify: `internal/mds/rpc/http.go`
- Modify: `internal/mds/rpc/http_test.go`

- [ ] **Step 1: Write failing handler tests for route metrics and request logging hooks**

Add tests that:

- hit `/uploads`, `/downloads/<id>`, `/files/<id>`, `/chunks/<id>`, `/rpc/<method>`
- assert route normalization is stable, for example `/downloads/:fileID` and `/chunks/:chunkID`
- assert request IDs are attached to request context or response header

Suggested test skeleton:

```go
func TestHTTPHandler_RecordsNormalizedRouteMetrics(t *testing.T) {}

func TestHTTPHandler_AssignsRequestIDWhenMissing(t *testing.T) {}
```

- [ ] **Step 2: Run the service handler tests and verify failure**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/gateway ./internal/datanode ./internal/mds/rpc -v
```

Expected:

- FAIL because middleware and request correlation are not wired yet

- [ ] **Step 3: Add middleware integration**

Implementation requirements:

- wrap each handler route with metrics middleware
- extract or generate `X-Request-ID`
- store request ID in context
- add request ID to response headers
- log one structured request summary per inbound request

Suggested route labels:

- gateway: `/healthz`, `/metrics`, `/uploads`, `/downloads/:fileID`, `/files/:fileID`
- datanode: `/healthz`, `/metrics`, `/chunks/:chunkID`, `/internal/replicate`
- mds: `/healthz`, `/metrics`, `/rpc/:method`

- [ ] **Step 4: Re-run handler tests**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/gateway ./internal/datanode ./internal/mds/rpc -v
```

Expected:

- PASS for new middleware tests
- existing handler tests remain green

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add internal/gateway/http.go internal/gateway/http_test.go internal/datanode/http.go internal/datanode/http_test.go internal/mds/rpc/http.go internal/mds/rpc/http_test.go
git commit -m "observability: add inbound request metrics and logging"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 4: Instrument Outbound HTTP Clients and Request Propagation

**Files:**
- Modify: `internal/gateway/client.go`
- Modify: `internal/gateway/http_test.go`
- Create: `internal/gateway/observability.go`
- Create: `internal/gateway/observability_test.go`
- Modify: `internal/datanode/mds_client.go`
- Modify: `internal/datanode/mds_client_test.go`
- Create: `internal/datanode/observability.go`
- Create: `internal/datanode/observability_test.go`

- [ ] **Step 1: Write failing tests for outbound metrics and `X-Request-ID` propagation**

Add tests that verify:

- gateway forwards `X-Request-ID` to MDS RPC and datanode HTTP requests
- gateway records upstream operation metrics with labels like `target=mds`, `operation=mds.start_upload`
- datanode MDS client records register and heartbeat results

Suggested test names:

```go
func TestUpstreamClient_ForwardsRequestID(t *testing.T) {}

func TestUpstreamClient_RecordsOperationMetrics(t *testing.T) {}

func TestMDSClient_RecordsRegisterAndHeartbeatMetrics(t *testing.T) {}
```

- [ ] **Step 2: Run focused client tests and verify failure**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/gateway ./internal/datanode -run 'Test(UpstreamClient|MDSClient)' -v
```

Expected:

- FAIL because propagation and instrumentation are not implemented

- [ ] **Step 3: Add outbound client instrumentation**

Implementation requirements:

- centralize request preparation so `X-Request-ID` is copied from context into outbound headers
- record per-operation duration and result counters
- keep gateway-specific metrics in `internal/gateway/observability.go`
- keep datanode-specific metrics in `internal/datanode/observability.go`

Suggested operations:

- gateway:
  - `health.mds`
  - `health.datanode`
  - `mds.create_file`
  - `mds.start_upload`
  - `mds.allocate_upload_targets`
  - `mds.commit_chunk`
  - `mds.complete_upload`
  - `mds.verify_upload`
  - `mds.build_download_plan`
  - `mds.get_node`
  - `mds.delete_file`
  - `datanode.put_chunk`
  - `datanode.get_chunk`
  - `datanode.delete_chunk`
  - `datanode.replicate_chunk`
- datanode:
  - `mds.register_node`
  - `mds.heartbeat_node`

- [ ] **Step 4: Re-run focused client tests**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/gateway ./internal/datanode -run 'Test(UpstreamClient|MDSClient)' -v
```

Expected:

- PASS for outbound instrumentation and propagation tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add internal/gateway/client.go internal/gateway/http_test.go internal/gateway/observability.go internal/gateway/observability_test.go internal/datanode/mds_client.go internal/datanode/mds_client_test.go internal/datanode/observability.go internal/datanode/observability_test.go
git commit -m "observability: instrument outbound service clients"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 5: Add Gateway Business Metrics

**Files:**
- Modify: `internal/gateway/http.go`
- Modify: `internal/gateway/http_test.go`
- Modify: `internal/gateway/observability.go`
- Modify: `internal/gateway/observability_test.go`

- [ ] **Step 1: Write failing tests for upload/download/delete counters**

Add tests that prove:

- successful upload increments request count, chunk count, and byte count
- failed download increments failure result count
- successful delete increments delete count

Suggested test names:

```go
func TestHTTPHandler_UploadRecordsBusinessMetrics(t *testing.T) {}

func TestHTTPHandler_DownloadFailureRecordsBusinessMetrics(t *testing.T) {}

func TestHTTPHandler_DeleteRecordsBusinessMetrics(t *testing.T) {}
```

- [ ] **Step 2: Run gateway tests and verify failure**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/gateway -v
```

Expected:

- FAIL because business counters are not recorded yet

- [ ] **Step 3: Add gateway business instrumentation**

Implementation requirements:

- increment upload request result once per `/uploads`
- increment upload chunk totals as each chunk commits
- observe upload bytes by original file size
- increment download result and total bytes returned
- increment delete result counters
- log business events with request ID and core identifiers

- [ ] **Step 4: Re-run gateway tests**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/gateway -v
```

Expected:

- PASS for new gateway observability tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add internal/gateway/http.go internal/gateway/http_test.go internal/gateway/observability.go internal/gateway/observability_test.go
git commit -m "observability: instrument gateway business flows"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 6: Add MDS RPC and Repairer Metrics

**Files:**
- Modify: `internal/mds/handler.go`
- Modify: `internal/mds/rpc/http.go`
- Modify: `internal/mds/rpc/http_test.go`
- Modify: `internal/mds/coordinator/repairer.go`
- Modify: `internal/mds/coordinator/repairer_test.go`
- Create: `internal/mds/observability.go`
- Create: `internal/mds/observability_test.go`

- [ ] **Step 1: Write failing tests for MDS RPC metrics and repairer outcomes**

Add tests that prove:

- one RPC call records method result and latency
- `register_node`, `heartbeat_node`, `start_upload`, `commit_chunk`, and `build_download_plan` increment the expected counters
- `RepairOnce` records attempted, succeeded, failed, and deferred replica totals

Suggested test names:

```go
func TestHTTPHandler_RecordsRPCMethodMetrics(t *testing.T) {}

func TestPendingReplicaRepairer_RecordsRepairMetrics(t *testing.T) {}
```

- [ ] **Step 2: Run focused MDS tests and verify failure**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/mds/rpc ./internal/mds/coordinator ./internal/mds -v
```

Expected:

- FAIL because MDS observability helpers do not exist yet

- [ ] **Step 3: Add MDS observability helpers and call sites**

Implementation requirements:

- create MDS-specific metrics holder in `internal/mds/observability.go`
- record per-RPC-method totals in `internal/mds/rpc/http.go`
- record business operation results in `internal/mds/handler.go` or another stable MDS boundary without polluting service logic
- instrument repair loop run start/end, duration, attempted/succeeded/failed/deferred counts
- generate a `run_id` for each repair cycle and include it in repair logs

- [ ] **Step 4: Re-run focused MDS tests**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/mds/rpc ./internal/mds/coordinator ./internal/mds -v
```

Expected:

- PASS for MDS RPC and repairer observability tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add internal/mds/handler.go internal/mds/rpc/http.go internal/mds/rpc/http_test.go internal/mds/coordinator/repairer.go internal/mds/coordinator/repairer_test.go internal/mds/observability.go internal/mds/observability_test.go
git commit -m "observability: instrument mds rpc and repair loop"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 7: Add Datanode Business Metrics and Heartbeat Metrics

**Files:**
- Modify: `internal/datanode/http.go`
- Modify: `internal/datanode/http_test.go`
- Modify: `internal/datanode/mds_client.go`
- Modify: `internal/datanode/mds_client_test.go`
- Modify: `cmd/datanode/app.go`
- Modify: `internal/datanode/observability.go`
- Modify: `internal/datanode/observability_test.go`

- [ ] **Step 1: Write failing tests for chunk, replicate, register, and heartbeat metrics**

Add tests that prove:

- `PUT/GET/DELETE /chunks/<id>` increment the expected result counters
- `/internal/replicate` tracks request result and per-target outcomes
- startup registration records success and failure
- heartbeat loop records success and failure

Suggested test names:

```go
func TestHTTPHandler_PutGetDeleteRecordMetrics(t *testing.T) {}

func TestHTTPHandler_ReplicateRecordsTargetMetrics(t *testing.T) {}

func TestMDSClient_HeartbeatRecordsMetrics(t *testing.T) {}
```

- [ ] **Step 2: Run focused datanode tests and verify failure**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/datanode ./cmd/datanode -v
```

Expected:

- FAIL because datanode business instrumentation is not complete yet

- [ ] **Step 3: Add datanode business instrumentation**

Implementation requirements:

- record chunk operation result counters at HTTP handler boundaries
- record replication request result and per-target totals
- record registration and heartbeat results from the MDS client and app loop
- log key datanode events with `chunk_id`, `node_id`, and request or run context when available

- [ ] **Step 4: Re-run focused datanode tests**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/datanode ./cmd/datanode -v
```

Expected:

- PASS for datanode observability tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add internal/datanode/http.go internal/datanode/http_test.go internal/datanode/mds_client.go internal/datanode/mds_client_test.go cmd/datanode/app.go internal/datanode/observability.go internal/datanode/observability_test.go
git commit -m "observability: instrument datanode operations"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 8: Documentation and Full Verification

**Files:**
- Modify: `docs/architecture/manual-testing.md`
- Modify: `docs/architecture/technical-debt-roadmap.md`

- [ ] **Step 1: Write failing or missing validation checklist items**

Before editing docs, list the manual checks that must be supported:

- `/metrics` can be reached on all three services
- upload increments gateway and MDS counters
- repair run changes repair metrics
- delete changes gateway and datanode counters

- [ ] **Step 2: Update operator-facing docs**

Document:

- how to curl `/metrics`
- which core metric families to expect
- how to verify request ID propagation during manual testing

Update the technical debt roadmap to record what remains out of scope:

- no tracing backend yet
- no alerting/dashboards yet
- no Redis/RabbitMQ/PostgreSQL cluster exporter integration yet
- no Kubernetes monitoring manifests yet

- [ ] **Step 3: Run targeted and full validation**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/platform/observability/... -v
GOCACHE=/tmp/go-cache go test ./internal/gateway ./internal/datanode ./internal/mds ./internal/mds/rpc ./internal/mds/coordinator ./cmd/gateway ./cmd/datanode ./cmd/mds -v
GOCACHE=/tmp/go-cache go test ./...
GOCACHE=/tmp/go-cache go build ./...
```

Expected:

- PASS for all tests
- PASS for full build

- [ ] **Step 4: Perform manual smoke verification**

Follow [manual-testing.md](/home/qingke/AstraStorage/docs/architecture/manual-testing.md) and confirm:

- each service returns Prometheus text on `/metrics`
- upload/download/delete still work
- one repair cycle updates repair metrics

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add docs/architecture/manual-testing.md docs/architecture/technical-debt-roadmap.md
git commit -m "observability: document metrics foundation"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Execution Notes

- Implement tasks in order.
- Do not collapse multiple tasks into one large patch.
- Keep each task independently testable.
- If Prometheus dependency download fails in the sandbox, request escalation rather than switching libraries.
- If any existing test becomes flaky because of log output or timing assumptions, fix the test deterministically instead of weakening observability behavior.
