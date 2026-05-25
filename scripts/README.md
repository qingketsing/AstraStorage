# Scripts

This directory holds operational and verification scripts.

## Layout

- `build-images.sh`: builds the `mds`, `datanode`, and `gateway` Docker images
- `poc-smoke.sh`: validates the core PoC gateway flow against an already running environment
- `smoke/`: smoke checks for local or deployment validation

## Current Scripts

- `build-images.sh`: local application image build entrypoint
- `poc-smoke.sh`: checks health, upload, metadata read, download, content equality, delete, and delete confirmation
- `smoke/monitoring-smoke.sh`: checks Prometheus and Alertmanager readiness plus key AstraStorage metrics
