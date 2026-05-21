# Prometheus Monitoring

## Scope

This document describes the first Prometheus-based monitoring foundation for AstraStorage.

The current goal is not to build a custom monitoring service. The goal is to use Prometheus as the metrics collection, storage, query, and alert evaluation layer for the existing service metrics.

The first monitored services are:

- `mds`
- `gateway`
- `datanode`

Each service already exposes Prometheus-compatible metrics on `/metrics`.

## Local Stack

Local monitoring files live in:

- [docker-compose.yml](/home/qingke/AstraStorage/deploy/docker/monitor/docker-compose.yml)
- [prometheus.yml](/home/qingke/AstraStorage/deploy/docker/monitor/prometheus.yml)
- [alerts.yml](/home/qingke/AstraStorage/deploy/docker/monitor/alerts.yml)
- [alertmanager.yml](/home/qingke/AstraStorage/deploy/docker/monitor/alertmanager.yml)

The compose stack starts:

- Prometheus on `http://127.0.0.1:9090`
- Alertmanager on `http://127.0.0.1:9093`

## Scrape Model

Prometheus uses pull-based scraping.

For local development it scrapes:

- `127.0.0.1:8080/metrics` for `mds`
- `127.0.0.1:11080/metrics` for `gateway`
- `127.0.0.1:10080/metrics` for `datanode`

The default local interval is `5s`, so new samples and alert evaluations update quickly during development.

## First Alert Rules

The first alert group intentionally covers only high-signal conditions:

- `AstraTargetDown`: Prometheus cannot scrape one of the service targets.
- `AstraMDSNoLeader`: no MDS instance reports active leadership.
- `AstraMDSLeaderFlapping`: MDS leadership changes too often.
- `AstraMDSRepairFailures`: the MDS repair loop reports failed runs.
- `AstraGatewayUpstreamFailures`: gateway calls to MDS or datanode fail repeatedly.
- `AstraGatewayUploadFailures`: upload requests fail repeatedly.
- `AstraDatanodeReplicationFailures`: datanode internal replication fails.
- `AstraDatanodeHeartbeatStale`: datanode has not reported a successful heartbeat recently.

The rules avoid high-cardinality labels. Request IDs and object IDs should remain in structured logs, not metric labels.

## Run Locally

Start application services in separate shells:

```bash
GOCACHE=/tmp/go-cache go run ./cmd/mds
DATANODE_MDS_HTTP_BASE_URL=http://127.0.0.1:8080 \
  GOCACHE=/tmp/go-cache \
  go run ./cmd/datanode

GATEWAY_MDS_HTTP_BASE_URL=http://127.0.0.1:8080 \
  GATEWAY_DATANODE_BASE_URL=http://127.0.0.1:10080 \
  GOCACHE=/tmp/go-cache \
  go run ./cmd/gateway
```

Start Prometheus and Alertmanager:

```bash
docker compose -f deploy/docker/monitor/docker-compose.yml up
```

Then open:

```text
http://127.0.0.1:9090/targets
http://127.0.0.1:9090/alerts
http://127.0.0.1:9093
```

Run the smoke check:

```bash
bash scripts/smoke/monitoring-smoke.sh
```

## Useful Queries

Check service scrape health:

```promql
up
```

Check MDS RPC traffic:

```promql
rate(astrastorage_mds_rpc_requests_total[5m])
```

Check gateway upload failures:

```promql
sum(increase(astrastorage_gateway_upload_requests_total{result!="success"}[5m]))
```

Check datanode replication failures:

```promql
sum(increase(astrastorage_datanode_replicate_requests_total{result="failure"}[5m]))
```

Check MDS leadership:

```promql
sum(astrastorage_mds_leader_is_leader)
```

Check datanode heartbeat freshness:

```promql
time() - max(astrastorage_datanode_last_heartbeat_timestamp_seconds)
```

## Role Of cmd/monitor

Prometheus should remain the metrics backend. A future `cmd/monitor` should not replace Prometheus.

The better role for `cmd/monitor` is:

- receive Alertmanager webhooks
- store alert events and state transitions
- expose an AstraStorage-specific health summary API
- link alerts to runbooks and recovery actions
- provide audit-friendly alert history

This keeps metrics collection and PromQL evaluation in Prometheus while leaving project-specific operational workflow in AstraStorage.

## Next Steps

Recommended next steps:

1. Use [Kubernetes Deployment](/home/qingke/AstraStorage/docs/architecture/kubernetes-deployment.md) for the Kubernetes `ServiceMonitor` and `PrometheusRule` path.
2. Add Grafana dashboards after the scrape and alert rules are stable.
3. Add Alertmanager receivers such as webhook, email, or chat integration only after the local alert behavior is validated.
