# AstraStorage PoC

## 1. Scope

This document defines the current AstraStorage PoC shape, startup path, and known boundaries.

The current PoC target is:

- one `mds` metadata service
- one PostgreSQL metadata backend
- one or more `datanode` instances
- one `gateway` as the demo entrypoint

The PoC demonstrates:

- directory creation
- small-file upload
- file metadata inspection
- chunk and replica inspection
- download plan inspection
- file download
- file deletion

## 2. Main Entry

For PoC demos, the primary external entry is `gateway`.

Current demo-friendly APIs:

- `POST /directories`
- `GET /directories/<inodeID>/children`
- `POST /uploads`
- `GET /files/<fileID>`
- `GET /files/<fileID>/chunks`
- `GET /files/<fileID>/download-plan`
- `GET /downloads/<fileID>`
- `DELETE /files/<fileID>`

## 3. Local Startup

### 3.1 Build Images

From the repository root:

```bash
bash scripts/build-images.sh
```

This builds:

- `astrastorage/mds:local`
- `astrastorage/datanode:local`
- `astrastorage/gateway:local`

### 3.2 Local Process Path

Start PostgreSQL first, then `mds`, then `datanode`, then `gateway`.

For a complete local verification path, use:

- [manual-testing.md](/home/qingke/AstraStorage/docs/architecture/manual-testing.md)

## 4. Kubernetes Startup

The current Kubernetes manifests live under:

- [deploy/k8s](/home/qingke/AstraStorage/deploy/k8s)

If you use locally built images on `minikube`, load them into the cluster node first:

```bash
minikube image load astrastorage/mds:local
minikube image load astrastorage/datanode:local
minikube image load astrastorage/gateway:local
```

If you use a remote registry instead, push the images first and update the manifests to reference that registry.

Apply in order:

```bash
kubectl apply -k deploy/k8s/base
kubectl apply -k deploy/k8s/postgres
kubectl apply -k deploy/k8s/mds
kubectl apply -k deploy/k8s/datanode
kubectl apply -k deploy/k8s/gateway
kubectl apply -k deploy/k8s/monitor
```

Current Kubernetes defaults:

- `mds` uses `postgres` as the default metadata backend
- `mds` reads `MDS_POSTGRES_DSN` from the in-cluster PostgreSQL secret
- PostgreSQL runs as a single-replica `StatefulSet`
- `datanode` runs as a single-replica `StatefulSet` with a PVC-backed data directory

Detailed Kubernetes notes live in:

- [kubernetes-deployment.md](/home/qingke/AstraStorage/docs/architecture/kubernetes-deployment.md)

## 5. Suggested Demo Flow

Recommended PoC flow:

1. Create a directory through `gateway`.
2. Upload a small file through `POST /uploads`.
3. Read file metadata through `GET /files/<fileID>`.
4. Inspect chunk placement through `GET /files/<fileID>/chunks`.
5. Inspect the generated download plan through `GET /files/<fileID>/download-plan`.
6. Download the file through `GET /downloads/<fileID>`.
7. Delete the file through `DELETE /files/<fileID>`.

## 6. Current Constraints

The current PoC is intentionally narrow. Known limits:

- `POST /uploads` is still `content_base64` based and suited to small files, smoke tests, and demos.
- The default Kubernetes `datanode` is now restart-safe for a single replica, but it is not yet a multi-datanode persistent topology with Pod-specific advertise identities.
- PostgreSQL is single-replica and PoC-only, not HA.
- Redis, RabbitMQ, and etcd are not part of the default PoC startup path.
- The project has monitoring foundations, but not a complete dashboard and alert delivery workflow.
- The repository now includes [poc-smoke.sh](/home/qingke/AstraStorage/scripts/poc-smoke.sh) for the core gateway flow, but it assumes the environment is already running and does not provision the cluster for you.

## 7. What This PoC Proves

This PoC is already enough to prove:

- `mds`, `datanode`, and `gateway` are connected end to end
- metadata can persist through PostgreSQL instead of staying only in memory
- upload, metadata query, download, and delete flows are wired together
- the Kubernetes path is moving from development topology toward a deliverable demo topology

It does not yet prove:

- production-grade upload API design
- multi-datanode persistent storage behavior in Kubernetes
- multi-MDS HA behavior in Kubernetes
- production security, ingress, or network isolation
