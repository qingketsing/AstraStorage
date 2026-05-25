#!/usr/bin/env bash
set -euo pipefail

GATEWAY_BASE_URL="${GATEWAY_BASE_URL:-http://127.0.0.1:11080}"
SMOKE_PARENT_ID="${SMOKE_PARENT_ID:-root}"
SMOKE_FILE_NAME="${SMOKE_FILE_NAME:-poc-smoke.txt}"
SMOKE_CONTENT="${SMOKE_CONTENT:-hello astra poc smoke}"

require_tool() {
  local tool="$1"
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "missing required tool: $tool" >&2
    exit 1
  fi
}

json_read() {
  local expression="$1"
  python3 -c "import json, sys; data=json.load(sys.stdin); print(${expression})"
}

tmpdir="$(mktemp -d)"
expected_file="${tmpdir}/expected.txt"
downloaded_file="${tmpdir}/downloaded.txt"
delete_check_file="${tmpdir}/deleted.json"

cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

require_tool curl
require_tool python3

printf '%s' "${SMOKE_CONTENT}" > "${expected_file}"
content_base64="$(python3 -c 'import base64, sys; print(base64.b64encode(sys.stdin.buffer.read()).decode())' < "${expected_file}")"

echo "==> health check"
health_response="$(curl -fsS "${GATEWAY_BASE_URL}/healthz")"
health_status="$(printf '%s' "${health_response}" | json_read 'data["status"]')"
if [[ "${health_status}" != "ok" ]]; then
  echo "health check returned unexpected status: ${health_status}" >&2
  exit 1
fi

echo "==> upload small file"
upload_response="$(
  curl -fsS -X POST "${GATEWAY_BASE_URL}/uploads" \
    -H 'Content-Type: application/json' \
    -d "{
      \"parent_id\": \"${SMOKE_PARENT_ID}\",
      \"name\": \"${SMOKE_FILE_NAME}\",
      \"content_type\": \"text/plain\",
      \"content_base64\": \"${content_base64}\"
    }"
)"
file_id="$(printf '%s' "${upload_response}" | json_read 'data["file_id"]')"
if [[ -z "${file_id}" ]]; then
  echo "upload response did not include file_id" >&2
  exit 1
fi

echo "==> query metadata"
metadata_response="$(curl -fsS "${GATEWAY_BASE_URL}/files/${file_id}")"
metadata_file_id="$(printf '%s' "${metadata_response}" | json_read 'data["File"]["ID"]')"
if [[ "${metadata_file_id}" != "${file_id}" ]]; then
  echo "metadata response returned unexpected file id: ${metadata_file_id}" >&2
  exit 1
fi

echo "==> download file"
curl -fsS -o "${downloaded_file}" "${GATEWAY_BASE_URL}/downloads/${file_id}"

echo "==> verify content"
if ! cmp -s "${expected_file}" "${downloaded_file}"; then
  echo "downloaded content does not match uploaded content" >&2
  exit 1
fi

echo "==> delete file"
delete_status="$(
  curl -sS -o /dev/null -w '%{http_code}' -X DELETE "${GATEWAY_BASE_URL}/files/${file_id}"
)"
if [[ "${delete_status}" != "204" ]]; then
  echo "delete returned unexpected status: ${delete_status}" >&2
  exit 1
fi

echo "==> confirm deletion"
deleted_status="$(
  curl -sS -o "${delete_check_file}" -w '%{http_code}' "${GATEWAY_BASE_URL}/files/${file_id}" || true
)"
if [[ "${deleted_status}" -lt 400 ]]; then
  echo "expected missing file after delete, got status ${deleted_status}" >&2
  exit 1
fi

echo "PoC smoke check completed."
