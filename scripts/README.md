# Scripts

This directory holds operational and verification scripts.

## Layout

- `build-images.sh`: builds the `mds`, `datanode`, and `gateway` Docker images
- `smoke/`: smoke checks for local or deployment validation

## Current Scripts

- `build-images.sh`: local application image build entrypoint
- `smoke/monitoring-smoke.sh`: checks Prometheus and Alertmanager readiness plus key AstraStorage metrics
