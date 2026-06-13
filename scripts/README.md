# Scripts

This directory holds operational and verification scripts.

## Layout

- `build-images.sh`: builds the `mds`, `datanode`, and `gateway` Docker images
- `deploy-k8s.sh`: builds local images, loads them into `kind` or `minikube`, applies Kubernetes manifests, and waits for rollout
- `poc-smoke.sh`: validates the core PoC gateway flow against an already running environment
- `smoke/`: smoke checks for local or deployment validation

## Current Scripts

- `build-images.sh`: local application image build entrypoint
- `deploy-k8s.sh`: one-command Kubernetes development deployment. Use `--smoke` to port-forward the gateway and run the PoC smoke flow after rollout, and `--with-monitor` when Prometheus Operator CRDs are installed.
- `poc-smoke.sh`: checks health, repeated upload, metadata read, chunk listing, download plan, download, content equality, delete, and delete confirmation
- `smoke/monitoring-smoke.sh`: checks Prometheus and Alertmanager readiness plus key AstraStorage metrics
