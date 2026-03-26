# Redis Platform Layer

This package hosts the Redis high-availability integration for AstraStorage.

## Topology

The current design uses two Sentinel-managed replication groups:

- `cache`
  serves hot read models such as file metadata, directory listings, download plans, node health snapshots, null-cache entries, and bloom-filter state.
- `coord`
  serves distributed locking, warmup coordination, invalidation events, and other cache-control workflows.

Each group is expected to run as:

- `1` master
- `2` replicas
- monitored by the shared Sentinel quorum in `deploy/docker/redis-sentinel`

## Design Rules

- PostgreSQL remains the source of truth.
- Redis stores derived read models and coordination state only.
- The MDS and future gateway integrations should discover masters through Sentinel rather than hard-coding Redis node addresses.
- Read-heavy cache traffic and coordination primitives are split across separate replication groups so lock pressure is isolated from hot cache traffic.
