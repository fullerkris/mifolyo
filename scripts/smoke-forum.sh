#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_URL="${MIFOLYO_BASE_URL:-http://localhost:8000}"
MANAGE_STACK=1

if [[ "${1:-}" == "--no-stack" ]]; then
  MANAGE_STACK=0
fi

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "${tmp_dir}"
  if [[ ${MANAGE_STACK} -eq 1 ]]; then
    bash "${ROOT_DIR}/scripts/fullstack.sh" down >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

wait_for_health() {
  local path="$1"
  local expected="$2"
  local status=""

  for _ in {1..40}; do
    status="$(curl -s -o /dev/null -w "%{http_code}" "${BASE_URL}${path}" || true)"
    if [[ "${status}" == "${expected}" ]]; then
      return 0
    fi
    sleep 2
  done

  printf 'Health check failed for %s, expected %s but got %s\n' "${path}" "${expected}" "${status}" >&2
  return 1
}

request_json() {
  local method="$1"
  local path="$2"
  local data_file="$3"
  local output_file="$4"
  local auth_token="${5:-}"

  local headers=(-H "Content-Type: application/json")
  if [[ -n "${auth_token}" ]]; then
    headers+=(-H "Authorization: Bearer ${auth_token}")
  fi

  local response
  if [[ -n "${data_file}" ]]; then
    response="$(curl -sS -X "${method}" "${BASE_URL}${path}" "${headers[@]}" --data @"${data_file}" -w $'\n%{http_code}')"
  else
    response="$(curl -sS -X "${method}" "${BASE_URL}${path}" "${headers[@]}" -w $'\n%{http_code}')"
  fi

  local status="${response##*$'\n'}"
  local body="${response%$'\n'*}"

  printf '%s' "${body}" >"${output_file}"
  printf '%s' "${status}"
}

if [[ ${MANAGE_STACK} -eq 1 ]]; then
  bash "${ROOT_DIR}/scripts/fullstack.sh" up
fi

wait_for_health "/api/health/live" "200"
wait_for_health "/api/health/ready" "200"

timestamp="$(date +%s)"
register_payload="${tmp_dir}/register.json"
cat >"${register_payload}" <<JSON
{"name":"Smoke User","email":"smoke-${timestamp}@example.com","password":"password123","password_confirmation":"password123"}
JSON

register_output="${tmp_dir}/register.out.json"
register_status="$(request_json POST "/api/auth/register" "${register_payload}" "${register_output}")"
if [[ "${register_status}" != "201" ]]; then
  printf 'Register failed with status %s\n' "${register_status}" >&2
  cat "${register_output}" >&2
  exit 1
fi

token="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["data"]["token"])' "${register_output}")"

community_payload="${tmp_dir}/community.json"
cat >"${community_payload}" <<JSON
{"name":"Smoke Community ${timestamp}","description":"Smoke test community"}
JSON

community_output="${tmp_dir}/community.out.json"
community_status="$(request_json POST "/api/communities" "${community_payload}" "${community_output}" "${token}")"
if [[ "${community_status}" != "201" ]]; then
  printf 'Create community failed with status %s\n' "${community_status}" >&2
  cat "${community_output}" >&2
  exit 1
fi

community_slug="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["data"]["slug"])' "${community_output}")"

post_payload="${tmp_dir}/post.json"
cat >"${post_payload}" <<JSON
{"community_slug":"${community_slug}","title":"Smoke Post ${timestamp}","content_type":"text","body":"Smoke body"}
JSON

post_output="${tmp_dir}/post.out.json"
post_status="$(request_json POST "/api/posts" "${post_payload}" "${post_output}" "${token}")"
if [[ "${post_status}" != "201" ]]; then
  printf 'Create post failed with status %s\n' "${post_status}" >&2
  cat "${post_output}" >&2
  exit 1
fi

post_id="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["data"]["id"])' "${post_output}")"

comment_payload="${tmp_dir}/comment.json"
cat >"${comment_payload}" <<JSON
{"body":"Smoke comment"}
JSON

comment_output="${tmp_dir}/comment.out.json"
comment_status="$(request_json POST "/api/posts/${post_id}/comments" "${comment_payload}" "${comment_output}" "${token}")"
if [[ "${comment_status}" != "201" ]]; then
  printf 'Create comment failed with status %s\n' "${comment_status}" >&2
  cat "${comment_output}" >&2
  exit 1
fi

vote_payload="${tmp_dir}/vote.json"
cat >"${vote_payload}" <<JSON
{"votable_type":"post","votable_id":${post_id},"value":1}
JSON

vote_output="${tmp_dir}/vote.out.json"
vote_status="$(request_json POST "/api/votes" "${vote_payload}" "${vote_output}" "${token}")"
if [[ "${vote_status}" != "200" ]]; then
  printf 'Vote failed with status %s\n' "${vote_status}" >&2
  cat "${vote_output}" >&2
  exit 1
fi

report_payload="${tmp_dir}/report.json"
cat >"${report_payload}" <<JSON
{"reportable_type":"post","reportable_id":${post_id},"reason":"Smoke report"}
JSON

report_output="${tmp_dir}/report.out.json"
report_status="$(request_json POST "/api/reports" "${report_payload}" "${report_output}" "${token}")"
if [[ "${report_status}" != "201" ]]; then
  printf 'Report creation failed with status %s\n' "${report_status}" >&2
  cat "${report_output}" >&2
  exit 1
fi

report_id="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["data"]["id"])' "${report_output}")"

mod_queue_output="${tmp_dir}/mod-queue.out.json"
mod_queue_status="$(request_json GET "/api/mod/queue" "" "${mod_queue_output}" "${token}")"
if [[ "${mod_queue_status}" != "200" ]]; then
  printf 'Mod queue failed with status %s\n' "${mod_queue_status}" >&2
  cat "${mod_queue_output}" >&2
  exit 1
fi

mod_action_payload="${tmp_dir}/mod-action.json"
cat >"${mod_action_payload}" <<JSON
{"target_type":"post","target_id":${post_id},"action":"lock","report_id":${report_id},"reason":"Smoke moderation"}
JSON

mod_action_output="${tmp_dir}/mod-action.out.json"
mod_action_status="$(request_json POST "/api/mod/actions" "${mod_action_payload}" "${mod_action_output}" "${token}")"
if [[ "${mod_action_status}" != "200" ]]; then
  printf 'Mod action failed with status %s\n' "${mod_action_status}" >&2
  cat "${mod_action_output}" >&2
  exit 1
fi

printf 'Smoke check passed. Core community flows are healthy.\n'
