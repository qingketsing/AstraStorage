# Kubernetes Assets

This directory groups Kubernetes manifests by component.

## Active Directories

- `base/`: namespace and shared bootstrap resources
- `postgres/`: PostgreSQL metadata backend for MDS
- `mds/`: metadata service deployment and service
- `datanode/`: datanode deployment and service
- `gateway/`: gateway deployment and service
- `monitor/`: `ServiceMonitor` and `PrometheusRule` resources

## Apply Order

```bash
kubectl apply -k deploy/k8s/base
kubectl apply -k deploy/k8s/postgres
kubectl apply -k deploy/k8s/mds
kubectl apply -k deploy/k8s/datanode
kubectl apply -k deploy/k8s/gateway
kubectl apply -k deploy/k8s/monitor
```
