#!/usr/bin/env bash
set -euo pipefail

PROMETHEUS_URL="${PROMETHEUS_URL:-http://127.0.0.1:9090}"
ALERTMANAGER_URL="${ALERTMANAGER_URL:-http://127.0.0.1:9093}"

check_url() {
  local name="$1"
  local url="$2"

  if ! curl -fsS "$url" >/dev/null; then
    echo "FAIL ${name}: ${url}"
    return 1
  fi
  echo "OK   ${name}: ${url}"
}

query_prometheus() {
  local query="$1"
  local encoded

  encoded="$(printf '%s' "$query" | jq -sRr @uri 2>/dev/null || printf '%s' "$query")"
  check_url "promql ${query}" "${PROMETHEUS_URL}/api/v1/query?query=${encoded}"
}

check_url "prometheus" "${PROMETHEUS_URL}/-/ready"
check_url "alertmanager" "${ALERTMANAGER_URL}/-/ready"

query_prometheus "up"
query_prometheus "astrastorage_mds_rpc_requests_total"
query_prometheus "astrastorage_gateway_upload_requests_total"
query_prometheus "astrastorage_datanode_chunk_put_total"
query_prometheus "astrastorage_mds_leader_is_leader"

echo "Monitoring smoke check completed."
