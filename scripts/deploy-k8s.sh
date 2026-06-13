#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  bash scripts/deploy-k8s.sh [options]

Options:
  --cluster auto|kind|minikube|none  Image loading target, default: auto
  --skip-build                       Do not build application images
  --skip-load                        Do not load images into a local cluster
  --with-monitor                     Apply Prometheus Operator resources
  --smoke                            Port-forward gateway and run scripts/poc-smoke.sh
  -h, --help                         Show this help

Environment:
  IMAGE_PREFIX  Docker image prefix, default: astrastorage
  IMAGE_TAG     Docker image tag, default: local
  NAMESPACE     Kubernetes namespace, default: astrastorage

Examples:
  bash scripts/deploy-k8s.sh
  bash scripts/deploy-k8s.sh --smoke
  bash scripts/deploy-k8s.sh --cluster kind --with-monitor
  bash scripts/deploy-k8s.sh --skip-build --skip-load
EOF
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

cluster="auto"
build_images=1
load_images=1
apply_monitor=0
run_smoke=0

image_prefix="${IMAGE_PREFIX:-astrastorage}"
image_tag="${IMAGE_TAG:-local}"
namespace="${NAMESPACE:-astrastorage}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --cluster)
      if [[ $# -lt 2 ]]; then
        echo "--cluster requires a value" >&2
        exit 1
      fi
      cluster="$2"
      shift 2
      ;;
    --skip-build)
      build_images=0
      shift
      ;;
    --skip-load)
      load_images=0
      shift
      ;;
    --with-monitor)
      apply_monitor=1
      shift
      ;;
    --smoke)
      run_smoke=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

require_tool() {
  local tool="$1"
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "missing required tool: ${tool}" >&2
    exit 1
  fi
}

detect_cluster() {
  if command -v kind >/dev/null 2>&1 && kind get clusters 2>/dev/null | grep -q .; then
    echo kind
    return
  fi
  if command -v minikube >/dev/null 2>&1 && minikube status >/dev/null 2>&1; then
    echo minikube
    return
  fi
  echo none
}

load_image() {
  local image="$1"

  case "${cluster}" in
    kind)
      require_tool kind
      kind load docker-image "${image}"
      ;;
    minikube)
      require_tool minikube
      minikube image load "${image}"
      ;;
    none)
      echo "==> skipping local cluster image load for ${image}"
      ;;
    *)
      echo "unsupported cluster target: ${cluster}" >&2
      exit 1
      ;;
  esac
}

apply_kustomization() {
  local path="$1"
  echo "==> applying ${path}"
  kubectl apply -k "${path}"
}

wait_rollout() {
  local resource="$1"
  echo "==> waiting for ${resource}"
  kubectl -n "${namespace}" rollout status "${resource}" --timeout=180s
}

run_gateway_smoke() {
  local pf_pid

  require_tool curl
  require_tool python3

  echo "==> starting gateway port-forward on 127.0.0.1:11080"
  kubectl -n "${namespace}" port-forward svc/astra-gateway 11080:11080 >/tmp/astra-gateway-port-forward.log 2>&1 &
  pf_pid="$!"
  trap 'kill "${pf_pid}" >/dev/null 2>&1 || true' RETURN

  for _ in $(seq 1 30); do
    if curl -fsS http://127.0.0.1:11080/healthz >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done

  curl -fsS http://127.0.0.1:11080/healthz >/dev/null
  GATEWAY_BASE_URL=http://127.0.0.1:11080 bash scripts/poc-smoke.sh
}

require_tool kubectl
if [[ "${build_images}" -eq 1 ]]; then
  require_tool docker
fi

if [[ "${cluster}" == "auto" ]]; then
  cluster="$(detect_cluster)"
  echo "==> detected cluster target: ${cluster}"
fi

if [[ "${build_images}" -eq 1 ]]; then
  IMAGE_PREFIX="${image_prefix}" IMAGE_TAG="${image_tag}" bash scripts/build-images.sh
fi

if [[ "${load_images}" -eq 1 ]]; then
  for component in mds datanode gateway; do
    load_image "${image_prefix}/${component}:${image_tag}"
  done
fi

apply_kustomization deploy/k8s/base
apply_kustomization deploy/k8s/postgres
apply_kustomization deploy/k8s/mds
apply_kustomization deploy/k8s/datanode
apply_kustomization deploy/k8s/gateway

if [[ "${apply_monitor}" -eq 1 ]]; then
  apply_kustomization deploy/k8s/monitor
fi

wait_rollout statefulset/astra-postgres
wait_rollout deployment/astra-mds
wait_rollout statefulset/astra-datanode
wait_rollout deployment/astra-gateway

echo "==> current AstraStorage resources"
kubectl get pods,svc,pvc -n "${namespace}"

if [[ "${run_smoke}" -eq 1 ]]; then
  run_gateway_smoke
fi

cat <<EOF
Kubernetes deployment completed.

Gateway:
  kubectl -n ${namespace} port-forward svc/astra-gateway 11080:11080
  curl http://127.0.0.1:11080/healthz

MDS:
  kubectl -n ${namespace} port-forward svc/astra-mds 8080:8080
  curl http://127.0.0.1:8080/healthz
EOF
