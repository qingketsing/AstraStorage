#!/usr/bin/env bash
set -euo pipefail

GATEWAY_BASE_URL="${GATEWAY_BASE_URL:-http://127.0.0.1:11080}"
SMOKE_PARENT_ID="${SMOKE_PARENT_ID:-root}"
SMOKE_FILE_NAME="${SMOKE_FILE_NAME:-poc-smoke.txt}"
SMOKE_CONTENT="${SMOKE_CONTENT:-hello astra poc smoke}"
SMOKE_RUNS="${SMOKE_RUNS:-2}"
SMOKE_LARGE_BYTES="${SMOKE_LARGE_BYTES:-4194432}"

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
delete_check_file="${tmpdir}/deleted.json"

cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

require_tool curl
require_tool python3

echo "==> health check"
health_response="$(curl -fsS "${GATEWAY_BASE_URL}/healthz")"
health_status="$(printf '%s' "${health_response}" | json_read 'data["status"]')"
if [[ "${health_status}" != "ok" ]]; then
  echo "health check returned unexpected status: ${health_status}" >&2
  exit 1
fi

run_case() {
  local case_name="$1"
  local expected_file="$2"
  local file_name="$3"
  local downloaded_file="${tmpdir}/${case_name}-downloaded.bin"
  local upload_response
  local file_id
  local metadata_response
  local metadata_file_id
  local chunks_response
  local chunk_count
  local plan_response
  local plan_chunk_count
  local upload_payload_file="${tmpdir}/${case_name}-upload.json"
  local delete_status
  local deleted_status

  python3 - "${expected_file}" "${SMOKE_PARENT_ID}" "${file_name}" > "${upload_payload_file}" <<'PY'
import base64
import json
import sys

with open(sys.argv[1], "rb") as fh:
    content_base64 = base64.b64encode(fh.read()).decode()

json.dump(
    {
        "parent_id": sys.argv[2],
        "name": sys.argv[3],
        "content_type": "application/octet-stream",
        "content_base64": content_base64,
    },
    sys.stdout,
)
PY

  echo "==> ${case_name}: upload file"
  upload_response="$(
    curl -fsS -X POST "${GATEWAY_BASE_URL}/uploads" \
      -H 'Content-Type: application/json' \
      --data-binary "@${upload_payload_file}"
  )"
  file_id="$(printf '%s' "${upload_response}" | json_read 'data["file_id"]')"
  if [[ -z "${file_id}" ]]; then
    echo "${case_name}: upload response did not include file_id" >&2
    exit 1
  fi

  echo "==> ${case_name}: query metadata"
  metadata_response="$(curl -fsS "${GATEWAY_BASE_URL}/files/${file_id}")"
  metadata_file_id="$(printf '%s' "${metadata_response}" | json_read 'data["File"]["ID"]')"
  if [[ "${metadata_file_id}" != "${file_id}" ]]; then
    echo "${case_name}: metadata response returned unexpected file id: ${metadata_file_id}" >&2
    exit 1
  fi

  echo "==> ${case_name}: query chunks"
  chunks_response="$(curl -fsS "${GATEWAY_BASE_URL}/files/${file_id}/chunks")"
  chunk_count="$(printf '%s' "${chunks_response}" | json_read 'len(data["Chunks"])')"
  if [[ "${chunk_count}" -lt 1 ]]; then
    echo "${case_name}: expected at least one chunk, got ${chunk_count}" >&2
    exit 1
  fi

  echo "==> ${case_name}: query download plan"
  plan_response="$(curl -fsS "${GATEWAY_BASE_URL}/files/${file_id}/download-plan")"
  plan_chunk_count="$(printf '%s' "${plan_response}" | json_read 'data["Plan"]["ChunkCount"]')"
  if [[ "${plan_chunk_count}" != "${chunk_count}" ]]; then
    echo "${case_name}: download plan chunk count ${plan_chunk_count} does not match chunks ${chunk_count}" >&2
    exit 1
  fi

  echo "==> ${case_name}: download file"
  curl -fsS -o "${downloaded_file}" "${GATEWAY_BASE_URL}/downloads/${file_id}"

  echo "==> ${case_name}: verify content"
  if ! cmp -s "${expected_file}" "${downloaded_file}"; then
    echo "${case_name}: downloaded content does not match uploaded content" >&2
    exit 1
  fi

  echo "==> ${case_name}: delete file"
  delete_status="$(
    curl -sS -o /dev/null -w '%{http_code}' -X DELETE "${GATEWAY_BASE_URL}/files/${file_id}"
  )"
  if [[ "${delete_status}" != "204" ]]; then
    echo "${case_name}: delete returned unexpected status: ${delete_status}" >&2
    exit 1
  fi

  echo "==> ${case_name}: confirm deletion"
  deleted_status="$(
    curl -sS -o "${delete_check_file}" -w '%{http_code}' "${GATEWAY_BASE_URL}/files/${file_id}" || true
  )"
  if [[ "${deleted_status}" -lt 400 ]]; then
    echo "${case_name}: expected missing file after delete, got status ${deleted_status}" >&2
    exit 1
  fi
}

small_file="${tmpdir}/small.txt"
printf '%s' "${SMOKE_CONTENT}" > "${small_file}"
run_case "small-file" "${small_file}" "${SMOKE_FILE_NAME}"

if [[ "${SMOKE_RUNS}" -ge 2 ]]; then
  large_file="${tmpdir}/large.bin"
  python3 - "${SMOKE_LARGE_BYTES}" > "${large_file}" <<'PY'
import sys
size = int(sys.argv[1])
pattern = b"astra-poc-smoke-"
written = 0
while written < size:
    chunk = pattern[: min(len(pattern), size - written)]
    sys.stdout.buffer.write(chunk)
    written += len(chunk)
PY
  run_case "multi-chunk-file" "${large_file}" "multi-${SMOKE_FILE_NAME}"
fi

echo "PoC smoke check completed."
