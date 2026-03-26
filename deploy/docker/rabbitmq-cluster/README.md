# RabbitMQ Cluster Stack

This stack provides the RabbitMQ high-availability topology for AstraStorage:

- `rabbitmq-1`
- `rabbitmq-2`
- `rabbitmq-3`

All three nodes join a single RabbitMQ cluster using config-file peer discovery.
The cluster exposes:

- AMQP on `5672`, `5673`, `5674`
- Management UI on `15672`, `15673`, `15674`
- Prometheus metrics on `15692`, `15693`, `15694`

Default credentials:

- user: `astra`
- password: `astra-dev`
- vhost: `/astra`

## Start

```bash
docker compose -f deploy/docker/rabbitmq-cluster/docker-compose.yml up -d
```

## Stop

```bash
docker compose -f deploy/docker/rabbitmq-cluster/docker-compose.yml down
```

## Status

```bash
docker compose -f deploy/docker/rabbitmq-cluster/docker-compose.yml ps
docker compose -f deploy/docker/rabbitmq-cluster/docker-compose.yml exec rabbitmq-1 rabbitmqctl cluster_status
```

## Management

- `http://127.0.0.1:15672`
- `http://127.0.0.1:15673`
- `http://127.0.0.1:15674`

## Notes

- The cluster uses an odd number of nodes, matching RabbitMQ's clustering guidance.
- Future task queues should use quorum queues rather than classic mirrored queues.

## Integration Tests

Set the test endpoints to the three AMQP ports exposed by this stack:

```bash
export MDS_TEST_RABBITMQ_ENDPOINTS=127.0.0.1:5672,127.0.0.1:5673,127.0.0.1:5674
export MDS_TEST_RABBITMQ_USERNAME=astra
export MDS_TEST_RABBITMQ_PASSWORD=astra-dev
export MDS_TEST_RABBITMQ_VHOST=/astra
```

Run the RabbitMQ integration suite:

```bash
GOCACHE=/tmp/go-cache go test ./test/integration -run TestRabbitMQ -v
```

The failover integration test temporarily stops `rabbitmq-cluster-rabbitmq-3-1` with `rabbitmqctl stop_app`
and restores it with `rabbitmqctl start_app` during cleanup.
