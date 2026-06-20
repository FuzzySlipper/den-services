#!/usr/bin/env bash
set -euo pipefail

gateway_url="${GATEWAY_URL:-http://127.0.0.1:8079}"
conversation_url="${CONVERSATION_URL:-http://127.0.0.1:8084}"
gateway_env_path="${GATEWAY_ENV_PATH:-/etc/den-services/gateway.env}"
database_url="${DEN_SERVICES_CANARY_DATABASE_URL:-${DEN_MIGRATION_DATABASE_URL:-}}"
run_id="${DEN_SERVICES_CANARY_RUN_ID:-pilot-canary-2917-$(date -u +%Y%m%dT%H%M%SZ)-$$}"
project_id="${DEN_SERVICES_CANARY_PROJECT_ID:-pilot-canary-2917}"
canary_header_name="X-Den-Migrated-Functions"
canary_header_value="true"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

load_env_file() {
  local path="$1"
  [[ -r "${path}" ]] || fail "gateway env file is not readable: ${path}"
  set -a
  # shellcheck disable=SC1090
  source "${path}"
  set +a
}

require_env() {
  local name="$1"
  [[ -n "${!name:-}" ]] || fail "required env var is missing: ${name}"
}

response_content_type() {
  local headers_path="$1"
  python3 - "${headers_path}" <<'PY'
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    for line in handle:
        if line.lower().startswith("content-type:"):
            print(line.split(":", 1)[1].strip())
            break
PY
}

request() {
  local method="$1"
  local url="$2"
  local token="$3"
  local use_canary="$4"
  local body_path="$5"
  local idempotency_key="$6"
  local out_prefix="$7"
  local headers_path="${out_prefix}.headers"
  local body_out_path="${out_prefix}.body"
  local args=(
    -sS
    -D "${headers_path}"
    -o "${body_out_path}"
    -w "%{http_code}"
    -X "${method}"
    -H "Authorization: Bearer ${token}"
  )

  if [[ "${use_canary}" == "true" ]]; then
    args+=(-H "${canary_header_name}: ${canary_header_value}")
  fi
  if [[ -n "${body_path}" ]]; then
    args+=(-H "Content-Type: application/json" --data-binary "@${body_path}")
  fi
  if [[ -n "${idempotency_key}" ]]; then
    args+=(-H "Idempotency-Key: ${idempotency_key}")
  fi

  curl "${args[@]}" "${url}"
}

json_field() {
  local body_path="$1"
  local field_path="$2"
  python3 - "${body_path}" "${field_path}" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    data = json.load(handle)

value = data
for part in sys.argv[2].split("."):
    if isinstance(value, list):
        value = value[int(part)]
    else:
        value = value.get(part)

if value is None:
    print("")
else:
    print(value)
PY
}

json_array_exact_message_count() {
  local body_path="$1"
  local message_id="$2"
  local expected_run_id="$3"
  python3 - "${body_path}" "${message_id}" "${expected_run_id}" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    messages = json.load(handle)

message_id = int(sys.argv[2])
run_id = sys.argv[3]
count = 0
for message in messages:
    metadata = message.get("metadata") or {}
    if (
        message.get("id") == message_id
        and message.get("source_kind") == "synthetic_canary"
        and metadata.get("run_id") == run_id
    ):
        count += 1
print(count)
PY
}

json_cursor_present() {
  local body_path="$1"
  local channel_id="$2"
  local message_id="$3"
  python3 - "${body_path}" "${channel_id}" "${message_id}" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    cursors = json.load(handle)

channel_id = int(sys.argv[2])
message_id = int(sys.argv[3])
for cursor in cursors:
    if (
        cursor.get("channel_id") == channel_id
        and cursor.get("reader_type") == "human"
        and cursor.get("reader_identity") == "synthetic-canary"
        and cursor.get("last_read_message_id") == message_id
    ):
        print("true")
        sys.exit(0)
print("false")
PY
}

database_counts() {
  [[ -n "${database_url}" ]] || fail "DEN_SERVICES_CANARY_DATABASE_URL or DEN_MIGRATION_DATABASE_URL is required for count checks"
  psql "${database_url}" -X -v ON_ERROR_STOP=1 -Atqc "
select
  'delivery_intents=' || (select count(*) from den_delivery.delivery_intents) ||
  ',runtime_subscriptions=' || (select count(*) from den_runtime.channel_subscriptions) ||
  ',runtime_subscription_cursors=' || (select count(*) from den_runtime.channel_subscription_cursors) ||
  ',observation_activity_events=' || (select count(*) from den_observation.activity_events);
"
}

write_json() {
  local output_path="$1"
  local kind="$2"
  python3 - "${output_path}" "${kind}" "${run_id}" "${project_id}" "${3:-}" "${4:-}" <<'PY'
import json
import sys

output_path = sys.argv[1]
kind = sys.argv[2]
run_id = sys.argv[3]
project_id = sys.argv[4]
channel_id = sys.argv[5]
message_id = sys.argv[6]

payloads = {
    "channel": {
        "slug": run_id,
        "display_name": f"Pilot canary 2917 {run_id}",
        "kind": "synthetic_canary",
        "project_id": project_id,
        "created_by": "synthetic-canary",
        "visibility": "normal",
        "settings": {"run_id": run_id},
    },
    "read_denied_channel": {
        "slug": f"{run_id}-read-denied",
        "display_name": f"Pilot canary denied {run_id}",
        "kind": "synthetic_canary",
        "project_id": project_id,
        "created_by": "synthetic-canary",
        "visibility": "normal",
        "settings": {"run_id": run_id, "purpose": "read-token-denied-check"},
    },
    "message": {
        "sender_type": "system",
        "sender_identity": "synthetic-canary",
        "body": f"synthetic canary run {run_id}",
        "message_kind": "system_event",
        "source_kind": "synthetic_canary",
        "source_id": run_id,
        "source_project_id": project_id,
        "target_project_id": project_id,
        "metadata": {"run_id": run_id, "task_id": 2917},
    },
    "cursor": {
        "reader_type": "human",
        "reader_identity": "synthetic-canary",
        "last_read_message_id": int(message_id) if message_id else None,
    },
    "reaction": {
        "reactor_type": "system",
        "reactor_identity": "synthetic-canary",
        "reaction": "synthetic_canary_ok",
    },
}

payload = payloads[kind]
if kind == "cursor" and payload["last_read_message_id"] is None:
    raise SystemExit("message id is required for cursor payload")

with open(output_path, "w", encoding="utf-8") as handle:
    json.dump(payload, handle, separators=(",", ":"))
PY
}

assert_status() {
  local actual="$1"
  local expected="$2"
  local label="$3"
  [[ "${actual}" == "${expected}" ]] || fail "${label}: status ${actual}, want ${expected}"
}

assert_json_content_type() {
  local content_type="$1"
  local label="$2"
  [[ "${content_type}" == application/json* ]] || fail "${label}: content-type ${content_type}, want application/json"
}

assert_legacy_catch_all() {
  local label="$1"
  local status
  local content_type
  local body_path="${tmp_dir}/${label}.body"
  local headers_path="${tmp_dir}/${label}.headers"

  status="$(request "GET" "${gateway_url}/v1/conversation/channels?limit=1" "${DEN_GATEWAY_SERVICE_TOKEN}" "false" "" "" "${tmp_dir}/${label}")"
  content_type="$(response_content_type "${headers_path}")"
  assert_status "${status}" "200" "${label}"
  if [[ "${content_type}" != text/html* ]] && ! grep -qi "den channels" "${body_path}"; then
    fail "${label}: response did not look like the legacy catch-all"
  fi
  echo "${label}_status=${status}"
  echo "${label}_content_type=${content_type}"
}

require_command curl
require_command psql
require_command python3
load_env_file "${gateway_env_path}"
require_env DEN_GATEWAY_SERVICE_TOKEN
require_env DEN_GATEWAY_CONVERSATION_READ_TOKEN
require_env DEN_GATEWAY_CONVERSATION_WRITE_TOKEN

echo "run_id=${run_id}"

counts_before="$(database_counts)"
echo "non_conversation_counts_before=${counts_before}"

gateway_version_status="$(request "GET" "${gateway_url}/version" "${DEN_GATEWAY_SERVICE_TOKEN}" "false" "" "" "${tmp_dir}/gateway-version")"
assert_status "${gateway_version_status}" "200" "gateway version"
echo "gateway_version_commit=$(json_field "${tmp_dir}/gateway-version.body" "commit")"

conversation_version_status="$(request "GET" "${conversation_url}/version" "${DEN_GATEWAY_SERVICE_TOKEN}" "false" "" "" "${tmp_dir}/conversation-version")"
assert_status "${conversation_version_status}" "200" "conversation version"
echo "conversation_version_commit=$(json_field "${tmp_dir}/conversation-version.body" "commit")"

assert_legacy_catch_all "legacy_initial"

read_status="$(request "GET" "${gateway_url}/v1/conversation/channels?project_id=${project_id}&kind=project_default&limit=2" "${DEN_GATEWAY_CONVERSATION_READ_TOKEN}" "true" "" "" "${tmp_dir}/read-channels")"
read_content_type="$(response_content_type "${tmp_dir}/read-channels.headers")"
assert_status "${read_status}" "200" "header gated channel read"
assert_json_content_type "${read_content_type}" "header gated channel read"
echo "successor_read_status=${read_status}"
echo "successor_read_content_type=${read_content_type}"

write_json "${tmp_dir}/read-denied-channel.json" "read_denied_channel"
read_denied_status="$(request "POST" "${gateway_url}/v1/conversation/channels" "${DEN_GATEWAY_CONVERSATION_READ_TOKEN}" "true" "${tmp_dir}/read-denied-channel.json" "" "${tmp_dir}/read-token-post-denied")"
assert_status "${read_denied_status}" "401" "read token channel write denial"
echo "read_token_post_channels_status=${read_denied_status}"

write_json "${tmp_dir}/channel.json" "channel"
create_status="$(request "POST" "${gateway_url}/v1/conversation/channels" "${DEN_GATEWAY_CONVERSATION_WRITE_TOKEN}" "true" "${tmp_dir}/channel.json" "" "${tmp_dir}/create-channel")"
assert_status "${create_status}" "201" "create synthetic channel"
channel_id="$(json_field "${tmp_dir}/create-channel.body" "id")"
[[ -n "${channel_id}" ]] || fail "create synthetic channel: missing id"
echo "synthetic_channel_id=${channel_id}"

write_json "${tmp_dir}/message.json" "message"
message_key="${run_id}-message"
append_status="$(request "POST" "${gateway_url}/v1/conversation/channels/${channel_id}/messages" "${DEN_GATEWAY_CONVERSATION_WRITE_TOKEN}" "true" "${tmp_dir}/message.json" "${message_key}" "${tmp_dir}/append-message")"
assert_status "${append_status}" "201" "append synthetic message"
message_id="$(json_field "${tmp_dir}/append-message.body" "id")"
[[ -n "${message_id}" ]] || fail "append synthetic message: missing id"

append_retry_status="$(request "POST" "${gateway_url}/v1/conversation/channels/${channel_id}/messages" "${DEN_GATEWAY_CONVERSATION_WRITE_TOKEN}" "true" "${tmp_dir}/message.json" "${message_key}" "${tmp_dir}/append-message-retry")"
assert_status "${append_retry_status}" "201" "append synthetic message retry"
message_retry_id="$(json_field "${tmp_dir}/append-message-retry.body" "id")"
[[ "${message_id}" == "${message_retry_id}" ]] || fail "append retry id ${message_retry_id}, want ${message_id}"
echo "synthetic_message_id=${message_id}"
echo "idempotent_retry_message_id=${message_retry_id}"

messages_status="$(request "GET" "${gateway_url}/v1/conversation/channels/${channel_id}/messages?limit=10" "${DEN_GATEWAY_CONVERSATION_READ_TOKEN}" "true" "" "" "${tmp_dir}/list-messages")"
messages_content_type="$(response_content_type "${tmp_dir}/list-messages.headers")"
assert_status "${messages_status}" "200" "list synthetic messages"
assert_json_content_type "${messages_content_type}" "list synthetic messages"
matching_message_count="$(json_array_exact_message_count "${tmp_dir}/list-messages.body" "${message_id}" "${run_id}")"
[[ "${matching_message_count}" == "1" ]] || fail "matching synthetic message count ${matching_message_count}, want 1"
echo "matching_synthetic_message_count=${matching_message_count}"

write_json "${tmp_dir}/cursor.json" "cursor" "${channel_id}" "${message_id}"
cursor_status="$(request "PUT" "${gateway_url}/v1/conversation/channels/${channel_id}/read-cursors" "${DEN_GATEWAY_CONVERSATION_WRITE_TOKEN}" "true" "${tmp_dir}/cursor.json" "" "${tmp_dir}/put-cursor")"
assert_status "${cursor_status}" "200" "put human read cursor"
cursor_message_id="$(json_field "${tmp_dir}/put-cursor.body" "last_read_message_id")"
[[ "${cursor_message_id}" == "${message_id}" ]] || fail "put cursor message id ${cursor_message_id}, want ${message_id}"
echo "put_human_cursor_status=${cursor_status}"

cursors_status="$(request "GET" "${gateway_url}/v1/conversation/channels/${channel_id}/read-cursors" "${DEN_GATEWAY_CONVERSATION_READ_TOKEN}" "true" "" "" "${tmp_dir}/list-cursors")"
cursors_content_type="$(response_content_type "${tmp_dir}/list-cursors.headers")"
assert_status "${cursors_status}" "200" "list human read cursors"
assert_json_content_type "${cursors_content_type}" "list human read cursors"
cursor_present="$(json_cursor_present "${tmp_dir}/list-cursors.body" "${channel_id}" "${message_id}")"
[[ "${cursor_present}" == "true" ]] || fail "human read cursor was not present in list response"
echo "human_cursor_present=${cursor_present}"

write_json "${tmp_dir}/reaction.json" "reaction"
reaction_status="$(request "POST" "${gateway_url}/v1/conversation/messages/${message_id}/reactions" "${DEN_GATEWAY_CONVERSATION_WRITE_TOKEN}" "true" "${tmp_dir}/reaction.json" "" "${tmp_dir}/add-reaction")"
if [[ "${reaction_status}" == "201" ]]; then
  reaction_id="$(json_field "${tmp_dir}/add-reaction.body" "id")"
  [[ -n "${reaction_id}" ]] || fail "add reaction: missing id"
  echo "reaction_status=${reaction_status}"
  echo "reaction_id=${reaction_id}"
elif [[ "${reaction_status}" == "400" ]]; then
  reaction_error_code="$(json_field "${tmp_dir}/add-reaction.body" "error.code")"
  if [[ "${reaction_error_code}" == "validation_failed" || "${reaction_error_code}" == "bad_request" ]]; then
    echo "reaction_status=${reaction_status}"
    echo "reaction_validation_code=${reaction_error_code}"
  else
    fail "reaction returned 400 with unexpected error code ${reaction_error_code}"
  fi
else
  fail "reaction status ${reaction_status}, want 201 or expected validation response"
fi

counts_after="$(database_counts)"
echo "non_conversation_counts_after=${counts_after}"
[[ "${counts_before}" == "${counts_after}" ]] || fail "non-conversation counts changed"

assert_legacy_catch_all "legacy_final"

echo "result=success"
