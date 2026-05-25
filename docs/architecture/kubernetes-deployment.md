# Kubernetes Deployment

## Scope

This document describes the first Kubernetes deployment foundation for AstraStorage.

The first Kubernetes target is a single-node, single-replica development topology:

- one PostgreSQL StatefulSet used as the MDS metadata backend
- one `mds` Deployment using the PostgreSQL metadata backend
- one single-replica `datanode` StatefulSet using a PVC
- one `gateway` Deployment
- Prometheus Operator integration through `ServiceMonitor` and `PrometheusRule`

This is intentionally not a production deployment. It is meant to validate Kubernetes networking, probes, service discovery, metadata persistence, metrics scraping, and the minimal upload/download path.

## Layout

Kubernetes manifests live under:

- [base](/home/qingke/AstraStorage/deploy/k8s/base)
- [postgres](/home/qingke/AstraStorage/deploy/k8s/postgres)
- [mds](/home/qingke/AstraStorage/deploy/k8s/mds)
- [datanode](/home/qingke/AstraStorage/deploy/k8s/datanode)
- [gateway](/home/qingke/AstraStorage/deploy/k8s/gateway)
- [monitor](/home/qingke/AstraStorage/deploy/k8s/monitor)

The namespace is:

```text
astrastorage
```

## Images

The manifests reference local development images:

```text
astrastorage/mds:local
astrastorage/datanode:local
astrastorage/gateway:local
```

For local `kind` or `minikube` clusters, build and load these images into the cluster node before applying the manifests.

For `minikube`:

```bash
minikube image load astrastorage/mds:local
minikube image load astrastorage/datanode:local
minikube image load astrastorage/gateway:local
```

For `kind`:

```bash
kind load docker-image astrastorage/mds:local
kind load docker-image astrastorage/datanode:local
kind load docker-image astrastorage/gateway:local
```

If you use a remote registry instead, push the images first and update the manifests to reference that registry.

## Deploy

Apply the manifests in order:

```bash
kubectl apply -k deploy/k8s/base
kubectl apply -k deploy/k8s/postgres
kubectl apply -k deploy/k8s/mds
kubectl apply -k deploy/k8s/datanode
kubectl apply -k deploy/k8s/gateway
```

Check workload status:

```bash
kubectl get pods -n astrastorage
kubectl get svc -n astrastorage
kubectl get pvc -n astrastorage
```

Wait for PostgreSQL before starting MDS validation:

```bash
kubectl -n astrastorage rollout status statefulset/astra-postgres
kubectl -n astrastorage rollout status deployment/astra-mds
```

The `mds` manifest now includes an `initContainer` that waits for PostgreSQL readiness, and the `datanode` manifest includes an `initContainer` that waits for `mds /healthz`. This removes the cold-start CrashLoopBackOff seen when the cluster comes up before its dependencies are ready.

## PostgreSQL Metadata Backend

The PostgreSQL manifests create:

- `Secret/astra-postgres` for the database username, password, and MDS DSN
- `ConfigMap/astra-postgres-config` for non-sensitive database and MDS pool settings
- `Service/astra-postgres` for stable in-cluster access on port `5432`
- `StatefulSet/astra-postgres` for the PostgreSQL process
- one `ReadWriteOnce` PVC through `volumeClaimTemplates`

MDS reads `MDS_POSTGRES_DSN` from `Secret/astra-postgres` and runs migrations during startup. This means MDS metadata survives MDS Pod restarts and PostgreSQL Pod restarts as long as the PVC is retained.

The default credentials are PoC-only:

```text
username: astra
password: astra-dev
database: astra
```

## Validate Services

Forward MDS:

```bash
kubectl -n astrastorage port-forward svc/astra-mds 8080:8080
```

Check:

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/metrics
```

Forward gateway:

```bash
kubectl -n astrastorage port-forward svc/astra-gateway 11080:11080
```

Check:

```bash
curl http://127.0.0.1:11080/healthz
```

Run the end-to-end PoC smoke check against the forwarded gateway:

```bash
bash scripts/poc-smoke.sh
```

## Prometheus Operator Integration

The monitor manifests require the Prometheus Operator CRDs:

- `servicemonitors.monitoring.coreos.com`
- `prometheusrules.monitoring.coreos.com`

If you use `kube-prometheus-stack`, install it first:

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  -n monitoring \
  --create-namespace
```

Apply monitoring resources:

```bash
kubectl apply -k deploy/k8s/monitor
```

Check:

```bash
kubectl get servicemonitor -n astrastorage
kubectl get prometheusrule -n astrastorage
```

The `ServiceMonitor` and `PrometheusRule` resources include:

```text
release: kube-prometheus-stack
```

This matches the common default selector used by the Helm chart. If your Prometheus Operator uses a different selector, adjust this label.

## Prometheus Validation

Forward Prometheus:

```bash
kubectl -n monitoring port-forward svc/kube-prometheus-stack-prometheus 9090:9090
```

Open:

```text
http://127.0.0.1:9090/targets
http://127.0.0.1:9090/alerts
```

Useful queries:

```promql
up{namespace="astrastorage"}
astrastorage_mds_rpc_requests_total{namespace="astrastorage"}
astrastorage_gateway_upload_requests_total{namespace="astrastorage"}
astrastorage_datanode_chunk_put_total{namespace="astrastorage"}
```

## Boundaries

This first version does not include:

- Redis
- RabbitMQ
- etcd-backed leader election
- multi-replica MDS
- multi-replica or Pod-identity-aware datanode topology
- HA PostgreSQL
- Ingress
- NetworkPolicy
- PodDisruptionBudget
- production-grade TLS or auth

Those should be added after the minimal Kubernetes path is verified.
