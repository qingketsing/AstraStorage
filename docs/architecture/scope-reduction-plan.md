# Scope Reduction Plan

## Purpose

This document records the features and default paths that should be removed, downgraded, or explicitly delayed.

The project is no longer in the earliest MVP stage. The goal is not to remove platform capabilities wholesale. The goal is to remove duplicate responsibilities, misleading defaults, demo-only interfaces, and paths that conflict with the semantics of a real distributed storage system.

The guiding target is:

```text
PostgreSQL as the metadata source of truth
Stateful datanode storage
streaming upload and download paths
one clear async task model
Prometheus ecosystem as the metrics and alerting foundation
```

## Priority Summary

Recommended execution order:

1. Remove `memory` backend as the default Kubernetes MDS backend.
2. Replace datanode `Deployment + emptyDir` as the default Kubernetes shape with `StatefulSet + PVC`.
3. Deprecate gateway `content_base64` as a formal upload path.
4. Narrow `cmd/monitor` and `internal/monitor` to alert event and health summary workflows.
5. Downgrade gRPC from a second formal external API until the control-plane contract stabilizes.
6. Collapse repair, cleanup, rebalance, and failover onto one async task path.
7. Keep Redis as an optional read-through cache, not a business-rule dependency.
8. Rework the empty monitor package plan so it does not imply a custom Prometheus replacement.

## 1. Remove Memory Backend As The Kubernetes Default

Decision:

`MDS_STORE_BACKEND=memory` should not be the default Kubernetes path.

Why this should be removed:

- Kubernetes pod restarts lose all metadata.
- A healthy-looking deployment can still have no durable source of truth.
- Multiple MDS replicas are meaningless with isolated in-memory state.
- Prometheus can report healthy services while the storage system has no persistent metadata.
- PostgreSQL repository code already exists, so the Kubernetes default should move toward the real metadata backend.

Replacement:

- Use PostgreSQL as the default Kubernetes backend.
- Keep `memory` for unit tests and quick local development.
- If a memory-based Kubernetes demo is still needed, put it in a `dev` overlay and name it as such.

Expected outcome:

Kubernetes deployments validate real restart behavior and make later multi-MDS work meaningful.

## 2. Replace Datanode Deployment And EmptyDir As The Default

Decision:

`Deployment + emptyDir` should not be the default datanode Kubernetes shape.

Why this should be removed:

- Datanode chunk storage is stateful.
- Pod rescheduling deletes `emptyDir` data.
- Replica health and repair semantics become misleading when local data disappears with the pod.
- Node identity is unstable under a plain Deployment.
- Capacity accounting is not reliable enough for a storage node default.

Replacement:

- Use `StatefulSet + PVC` as the default datanode shape.
- Use stable pod identity for `DATANODE_NODE_ID`.
- Keep `Deployment + emptyDir` only as a development overlay.

Expected outcome:

The data plane behaves like a storage system rather than a disposable stateless service.

## 3. Deprecate Gateway content_base64 Upload As A Formal Path

Decision:

The `content_base64` upload request should be treated as a smoke-test and small-file compatibility path, not the formal upload API.

Why this should be removed:

- Base64 expands payload size.
- Gateway has to hold the full file in memory.
- The path does not model real streaming upload behavior.
- Large files will pressure gateway memory.
- It conflicts with the system's chunk-oriented storage model.

Replacement:

- Add a streaming upload path.
- Keep `content_base64` for tests, demos, and backward compatibility.
- Document it as deprecated or non-production once the streaming path exists.

Expected outcome:

The gateway data path aligns with chunked distributed storage instead of an in-memory demo interface.

## 4. Narrow cmd/monitor And internal/monitor

Decision:

Do not build `cmd/monitor` as a full metrics collector, rule evaluator, dashboard backend, or alert sender.

Why this should be removed:

- Prometheus already handles metrics collection, storage, querying, and alert rule evaluation.
- Alertmanager already handles routing and notification integration.
- Rebuilding these parts creates duplicate facts and duplicate rules.
- A custom monitoring stack would add maintenance cost without improving storage semantics.

Replacement:

Use monitor only for AstraStorage-specific operational workflows:

- Alertmanager webhook receiver.
- Alert event storage and state transitions.
- Health summary API.
- Runbook links.
- Audit-friendly alert history.

Expected outcome:

Prometheus remains the metrics and alerting foundation, while `monitor` becomes a storage-specific operations layer.

## 5. Downgrade gRPC As A Second Formal External API

Decision:

gRPC should not be treated as an equal-priority formal external API until the control-plane contract stabilizes.

Why this should be removed:

- Dual protocols double the compatibility and testing surface.
- HTTP and gRPC mappings can drift.
- Error semantics need to be maintained twice.
- The gateway and current manual workflows already use HTTP.
- Protocol work can distract from metadata correctness and data-plane behavior.

Replacement:

- Keep existing gRPC code and tests.
- Mark it as experimental or internal until the API is stable.
- Promote it later only if SDK, performance, or cross-language contract requirements justify it.

Expected outcome:

Control-plane API semantics can evolve faster with one formal external path.

## 6. Collapse Background Repair And Coordination Onto One Task Path

Decision:

The system should not keep direct background execution and RabbitMQ task execution as two equally formal paths.

Why this should be removed:

- Retry behavior can diverge.
- Idempotency can diverge.
- Metrics become harder to explain.
- Failure recovery and DLQ handling are split.
- Tests need to cover two behaviorally similar but operationally different systems.

Replacement:

- Make RabbitMQ the formal task path for repair, cleanup, rebalance, and failover if asynchronous coordination is the chosen direction.
- Keep local direct execution only as a development or no-MQ fallback.
- Document which path is authoritative.

Expected outcome:

The control-plane coordination model becomes easier to operate, test, and observe.

## 7. Keep Redis Read Cache Optional And Non-Authoritative

Decision:

Redis should not become a required dependency for business correctness or service-layer invariants.

Why this should be reduced:

- Cache invalidation can obscure metadata bugs.
- Service logic becomes harder to reason about when cache behavior leaks into business decisions.
- Redis Sentinel adds operational complexity.
- PostgreSQL should remain the source of truth for metadata.

Replacement:

- Keep the `ReadCache` interface.
- Keep Redis as an optional read-through cache.
- Do not let business rules depend on cache hits, hotspot state, bloom filters, or warmup behavior.
- Use Kubernetes overlays or values to enable Redis only where needed.

Expected outcome:

Cache improves read performance without changing correctness semantics.

## 8. Rework The Empty Monitor Package Plan

Decision:

The monitor directory should not expand into a generic monitoring platform layout.

Why this should be removed:

The current planned shape implies a custom platform:

```text
collector/
exporter/
ingest/
rules/
storage/
notifier/
```

That overlaps with Prometheus and Alertmanager.

Replacement:

Use a narrower operations-oriented structure:

```text
internal/monitor/
├── alert/
├── health/
├── runbook/
└── api/
```

Expected outcome:

The package name matches its actual value: AstraStorage-specific operational context on top of the Prometheus ecosystem.

## What Should Not Be Removed

The following should stay in scope:

- MDS metadata model and invariants.
- Store interfaces and explicit transaction boundaries.
- PostgreSQL repository implementation.
- Gateway to MDS to datanode upload, download, and delete paths.
- Datanode chunk persistence and replication RPC.
- Health checks, metrics, structured logs, and request ID propagation.
- Prometheus, Alertmanager, ServiceMonitor, and PrometheusRule.
- RabbitMQ if it becomes the single formal async task path.
- Redis as optional read-through cache.
- etcd leader election once MDS runs with a durable shared backend.

## Review Rule

Before adding a new feature, ask:

```text
Does this strengthen the real storage path, the metadata source of truth, durability, recovery, or observability?
```

If the answer is no, the feature should be delayed or removed from the default path.
