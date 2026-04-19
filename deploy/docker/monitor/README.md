# AstraStorage Local Monitoring

This directory provides a local Prometheus and Alertmanager stack for the existing AstraStorage `/metrics` endpoints.

## Targets

Default scrape targets:

- MDS: `127.0.0.1:8080/metrics`
- Gateway: `127.0.0.1:11080/metrics`
- Datanode: `127.0.0.1:10080/metrics`

The compose file uses `network_mode: host`, so it is intended for local Linux development. If your Docker environment does not support host networking, change the targets in `prometheus.yml` to a reachable host address such as `host.docker.internal`.

## Start Services

Run the application services in separate shells:

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

Start monitoring:

```bash
docker compose -f deploy/docker/monitor/docker-compose.yml up
```

Open:

- Prometheus: `http://127.0.0.1:9090`
- Alertmanager: `http://127.0.0.1:9093`

## Validate

Check targets:

```text
http://127.0.0.1:9090/targets
```

Useful PromQL queries:

```promql
up
astrastorage_mds_rpc_requests_total
astrastorage_gateway_upload_requests_total
astrastorage_datanode_chunk_put_total
astrastorage_mds_leader_is_leader
```

Check alert state:

```text
http://127.0.0.1:9090/alerts
http://127.0.0.1:9093
```

You can also run the smoke check after Prometheus and Alertmanager are up:

```bash
bash scripts/monitoring-smoke.sh
```
