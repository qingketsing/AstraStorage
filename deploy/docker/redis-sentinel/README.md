# Redis Sentinel Stack

This stack provides the Redis high-availability topology used by AstraStorage:

- `cache` replication group
  - `redis-cache-master`
  - `redis-cache-replica-1`
  - `redis-cache-replica-2`
- `coord` replication group
  - `redis-coord-master`
  - `redis-coord-replica-1`
  - `redis-coord-replica-2`
- `5` Sentinel nodes
  - `sentinel-1` through `sentinel-5`

## Start

```bash
docker compose -f deploy/docker/redis-sentinel/docker-compose.yml up -d
```

## Stop

```bash
docker compose -f deploy/docker/redis-sentinel/docker-compose.yml down
```

## Sentinel Endpoints

Use the following Sentinel addresses from the host:

- `127.0.0.1:26379`
- `127.0.0.1:26380`
- `127.0.0.1:26381`
- `127.0.0.1:26382`
- `127.0.0.1:26383`

Master set names:

- `astra-cache`
- `astra-coord`

## Integration Test

The Sentinel stack returns Redis master addresses using Docker-network service
DNS, so the end-to-end Sentinel test should be executed from a container on the
same Docker network.

```bash
docker run --rm \
  --network redis-sentinel_default \
  -v "$PWD":/workspace \
  -w /workspace \
  golang:1.24 \
  bash -lc 'env MDS_TEST_REDIS_SENTINELS=sentinel-1:26379,sentinel-2:26379,sentinel-3:26379,sentinel-4:26379,sentinel-5:26379 GOCACHE=/tmp/go-cache go test ./test/integration -run TestRedisSentinelIntegration_MDSReadCacheAndWarmup -v'
```

This verifies:

- Sentinel-managed cache and coord clients can connect
- distributed locks work on the coord group
- MDS file/download-plan/directory/node/healthy-node caches populate through Redis
- warmup can repopulate hot read models
