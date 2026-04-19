# Kubernetes Deployment

## Scope

This document describes the first Kubernetes deployment foundation for AstraStorage.

The first Kubernetes target is a single-node, single-replica development topology:

- one `mds` Deployment using the in-memory metadata backend
- one `datanode` Deployment using `emptyDir`
- one `gateway` Deployment
- Prometheus Operator integration through `ServiceMonitor` and `PrometheusRule`

This is intentionally not a production deployment. It is meant to validate Kubernetes networking, probes, service discovery, metrics scraping, and the minimal upload/download path.

## Layout

Kubernetes manifests live under:

- [base](/home/qingke/AstraStorage/deploy/k8s/base)
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

For local `kind` clusters, build and load these images before applying the manifests.

## Deploy

Apply the manifests in order:

```bash
kubectl apply -k deploy/k8s/base
kubectl apply -k deploy/k8s/mds
kubectl apply -k deploy/k8s/datanode
kubectl apply -k deploy/k8s/gateway
```

Check workload status:

```bash
kubectl get pods -n astrastorage
kubectl get svc -n astrastorage
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

- PostgreSQL-backed MDS
- Redis
- RabbitMQ
- etcd-backed leader election
- multi-replica MDS
- StatefulSet and PVC-backed datanodes
- Ingress
- NetworkPolicy
- PodDisruptionBudget
- production-grade TLS or auth

Those should be added after the minimal Kubernetes path is verified.
