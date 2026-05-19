# AstraStorage Application Images

This directory contains Dockerfiles for the three AstraStorage application binaries.

## Images

- `Dockerfile.mds` builds `astrastorage/mds:local`
- `Dockerfile.datanode` builds `astrastorage/datanode:local`
- `Dockerfile.gateway` builds `astrastorage/gateway:local`

The Kubernetes manifests in [deploy/k8s](/home/qingke/AstraStorage/deploy/k8s) reference these image names.

## Build

From the repository root:

```bash
docker build -f deploy/docker/app/Dockerfile.mds -t astrastorage/mds:local .
docker build -f deploy/docker/app/Dockerfile.datanode -t astrastorage/datanode:local .
docker build -f deploy/docker/app/Dockerfile.gateway -t astrastorage/gateway:local .
```

For `kind` clusters:

```bash
kind load docker-image astrastorage/mds:local
kind load docker-image astrastorage/datanode:local
kind load docker-image astrastorage/gateway:local
```

## Runtime Defaults

The images do not bake in deployment-specific configuration. Use environment variables from the service configs and Kubernetes manifests:

- `MDS_HTTP_ADDR`
- `MDS_GRPC_ADDR`
- `MDS_STORE_BACKEND`
- `MDS_POSTGRES_DSN`
- `DATANODE_HTTP_ADDR`
- `DATANODE_DATA_DIR`
- `DATANODE_MDS_HTTP_BASE_URL`
- `DATANODE_ADVERTISE_URL`
- `GATEWAY_HTTP_ADDR`
- `GATEWAY_MDS_HTTP_BASE_URL`
- `GATEWAY_DATANODE_BASE_URL`
