#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  bash scripts/build-images.sh [mds|datanode|gateway ...]

Environment:
  IMAGE_PREFIX     Docker image prefix, default: astrastorage
  IMAGE_TAG        Docker image tag, default: local
  DOCKER_PLATFORM  Optional docker build --platform value

Examples:
  bash scripts/build-images.sh
  IMAGE_TAG=dev bash scripts/build-images.sh
  IMAGE_PREFIX=ghcr.io/example IMAGE_TAG=latest bash scripts/build-images.sh mds gateway
EOF
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

image_prefix="${IMAGE_PREFIX:-astrastorage}"
image_tag="${IMAGE_TAG:-local}"
docker_platform="${DOCKER_PLATFORM:-}"

declare -A dockerfiles=(
  [mds]="deploy/docker/app/Dockerfile.mds"
  [datanode]="deploy/docker/app/Dockerfile.datanode"
  [gateway]="deploy/docker/app/Dockerfile.gateway"
)

build_component() {
  local component="$1"
  local dockerfile="${dockerfiles[$component]:-}"
  local image="${image_prefix}/${component}:${image_tag}"
  local args=()

  if [[ -z "${dockerfile}" ]]; then
    echo "unknown component: ${component}" >&2
    usage >&2
    exit 1
  fi
  if [[ -n "${docker_platform}" ]]; then
    args+=(--platform "${docker_platform}")
  fi

  echo "==> building ${image}"
  docker build "${args[@]}" -f "${dockerfile}" -t "${image}" .
}

components=("$@")
if [[ ${#components[@]} -eq 0 ]]; then
  components=(mds datanode gateway)
fi

for component in "${components[@]}"; do
  case "${component}" in
    -h|--help)
      usage
      exit 0
      ;;
  esac
done

for component in "${components[@]}"; do
  build_component "${component}"
done
