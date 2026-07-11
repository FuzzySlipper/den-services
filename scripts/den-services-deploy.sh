#!/usr/bin/env bash
set -euo pipefail

service="gateway"
repo_root=""
pull_repo="true"
version="${DEN_SERVICES_VERSION:-0.1.0}"

usage() {
  cat <<'USAGE'
Usage:
  scripts/den-services-deploy.sh [service] [--repo PATH] [--pull|--no-pull]

Services are read from deployment/services.yaml.

Builds the registered service binary with version metadata, runs tests, installs
to /data/services/<service>, restarts den-go@<service>.service, and smokes
/health plus /version. Rollback is attempted if post-restart smoke checks fail.
USAGE
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "${script_dir}/../go.mod" ]]; then
  repo_root="$(cd "${script_dir}/.." && pwd)"
elif [[ -f /data/services/den-services/go.mod ]]; then
  repo_root="/data/services/den-services"
else
  repo_root="$(pwd)"
fi

if [[ $# -gt 0 && "${1}" != --* ]]; then
  service="$1"
  shift
fi

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      repo_root="${2:-}"
      if [[ -z "${repo_root}" ]]; then
        echo "--repo requires a path" >&2
        exit 2
      fi
      shift 2
      ;;
    --pull)
      pull_repo="true"
      shift
      ;;
    --no-pull)
      pull_repo="false"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 2
      ;;
  esac
done

load_service_metadata() {
  local registry_path="${repo_root}/deployment/services.yaml"
  if [[ ! -f "${registry_path}" ]]; then
    echo "missing deployment registry: ${registry_path}" >&2
    exit 1
  fi
  python3 - "${registry_path}" "${service}" <<'PY'
import shlex
import sys

registry_path = sys.argv[1]
target_name = sys.argv[2]
services = []
current = None

with open(registry_path, "r", encoding="utf-8") as handle:
    for raw_line in handle:
        line = raw_line.split("#", 1)[0].rstrip()
        if not line.strip():
            continue
        stripped = line.strip()
        if stripped.startswith("- "):
            if current is not None:
                services.append(current)
            current = {}
            stripped = stripped[2:].strip()
            if stripped:
                key, _, value = stripped.partition(":")
                current[key.strip()] = value.strip().strip('"').strip("'")
            continue
        if current is None or ":" not in stripped:
            continue
        key, _, value = stripped.partition(":")
        current[key.strip()] = value.strip().strip('"').strip("'")

if current is not None:
    services.append(current)

matches = [service for service in services if service.get("name") == target_name]
if len(matches) != 1:
    names = ", ".join(service.get("name", "<unnamed>") for service in services)
    print(f"unknown service {target_name}; registry services: {names}", file=sys.stderr)
    sys.exit(2)

service = matches[0]
required = [
    "module",
    "binary_name",
    "binary_path",
    "config_example",
    "env_example",
    "health_url",
    "version_url",
    "systemd_unit",
]
missing = [key for key in required if not service.get(key)]
if missing:
    print(f"service {target_name} missing registry fields: {', '.join(missing)}", file=sys.stderr)
    sys.exit(1)

for key in required:
    print(f"{key}={shlex.quote(service[key])}")
PY
}

eval "$(load_service_metadata)"

unit="${systemd_unit}"
service_root="/data/services/${service}"

run_systemctl() {
  if [[ "${EUID}" -eq 0 ]]; then
    "$@"
  else
    sudo -n "$@"
  fi
}

retry_curl() {
  local url="$1"
  local last_output=""
  for _ in {1..20}; do
    if last_output="$(curl -fsS "${url}" 2>&1)"; then
      printf '%s' "${last_output}"
      return 0
    fi
    sleep 1
  done
  echo "${last_output}" >&2
  return 1
}

json_field() {
  local field="$1"
  python3 -c 'import json,sys; print(json.load(sys.stdin).get(sys.argv[1], ""))' "${field}"
}

rollback() {
  if [[ -x "${service_root}/bin/${binary_name}.previous" ]]; then
    echo "Smoke failed; rolling back ${unit}" >&2
    install -m 0755 "${service_root}/bin/${binary_name}.previous" "${service_root}/bin/${binary_name}"
    run_systemctl /bin/systemctl restart "${unit}"
  fi
}

backup_config_file() {
  local path="$1"
  local name="$2"

  install -m 0644 "${path}" "${service_root}/backups/${name}.${built_at//[:-]/}"
}

route_operation_exists() {
  local routes_target="$1"
  local operation="$2"

  grep -Eq "operation:[[:space:]]*['\"]?${operation}['\"]?([[:space:]]|$)" "${routes_target}"
}

append_mcp_route_if_missing() {
  local routes_target="$1"
  local operation="$2"
  local backend="$3"
  local method="$4"
  local path="$5"
  local request_adapter="$6"
  local response_adapter="$7"
  local timeout="${8:-}"

  if route_operation_exists "${routes_target}" "${operation}"; then
    return 0
  fi

  backup_config_file "${routes_target}" "routes.yaml"

  local route_indent
  route_indent="$(awk '
    /^[[:space:]]*- operation:/ { match($0, /^[[:space:]]*/); print substr($0, RSTART, RLENGTH); found=1; exit }
    END { if (!found) print "__missing__" }
  ' "${routes_target}")"
  if [[ "${route_indent}" == "__missing__" ]]; then
    route_indent="  "
  fi

  local child_indent="${route_indent}  "
  local write_target="${routes_target}"
  local staged_routes=""
  if [[ ! -w "${routes_target}" ]]; then
    staged_routes="$(mktemp /tmp/den-mcp-routes.XXXXXX)"
    cp "${routes_target}" "${staged_routes}"
    write_target="${staged_routes}"
  fi
  {
    printf '\n'
    printf '%s- operation: "%s"\n' "${route_indent}" "${operation}"
    printf '%sbackend: "%s"\n' "${child_indent}" "${backend}"
    printf '%smethod: "%s"\n' "${child_indent}" "${method}"
    printf '%spath: "%s"\n' "${child_indent}" "${path}"
    printf '%srequest_adapter: "%s"\n' "${child_indent}" "${request_adapter}"
    printf '%sresponse_adapter: "%s"\n' "${child_indent}" "${response_adapter}"
    if [[ -n "${timeout}" ]]; then
      printf '%stimeout: "%s"\n' "${child_indent}" "${timeout}"
    fi
  } >> "${write_target}"
  if [[ -n "${staged_routes}" ]]; then
    run_systemctl install -m 0644 "${staged_routes}" "${routes_target}"
    rm -f "${staged_routes}"
  fi
}

install_mcp_routes() {
  local routes_target="${service_root}/config/routes.yaml"

  if [[ ! -f "${routes_target}" ]]; then
    install -m 0644 mcp/routes.example.yaml "${routes_target}"
    return 0
  fi

  if [[ "${DEN_MCP_REPLACE_ROUTES:-false}" == "true" ]]; then
    if ! cmp -s mcp/routes.example.yaml "${routes_target}"; then
      backup_config_file "${routes_target}" "routes.yaml"
    fi
    install -m 0644 mcp/routes.example.yaml "${routes_target}"
    return 0
  fi

  append_mcp_route_if_missing \
    "${routes_target}" \
    "await_github_checks" \
    "review" \
    "POST" \
    "/v1/projects/{project_id}/tasks/{task_id}/review/github-check-gates" \
    "mcp_review_rest" \
    "mcp_tool_result_json"
  append_mcp_route_if_missing "${routes_target}" "watch_github_checks" "review" "POST" \
    "/v1/projects/{project_id}/tasks/{task_id}/review/github-check-gates" "mcp_review_rest" "mcp_tool_result_json"
  append_mcp_route_if_missing "${routes_target}" "get_github_check_gate" "review" "GET" \
    "/v1/projects/{project_id}/tasks/{task_id}/review/github-check-gates/{commit_sha}" "mcp_review_rest" "mcp_tool_result_json"
  append_mcp_route_if_missing "${routes_target}" "wait_for_github_checks" "review" "GET" \
    "/v1/projects/{project_id}/tasks/{task_id}/review/github-check-gates/{commit_sha}/wait" "mcp_review_rest" "mcp_tool_result_json" "55s"
  append_mcp_route_if_missing "${routes_target}" "get_task_context" "tasks" "GET" \
    "/v1/tasks/{task_id}/context" "mcp_task_context_compose" "mcp_tool_result_json"
}

cd "${repo_root}"
if [[ ! -f go.mod ]]; then
  echo "repo path ${repo_root} does not contain go.mod" >&2
  exit 1
fi

if [[ "${pull_repo}" == "true" ]]; then
  git pull --ff-only
fi

if [[ -n "$(git status --porcelain)" ]]; then
  echo "refusing to deploy from dirty working tree" >&2
  git status --short >&2
  exit 1
fi

commit="$(git rev-parse --short=12 HEAD)"
built_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
stage_dir="$(mktemp -d "/tmp/den-services-${service}.XXXXXX")"

echo "Testing ${service} from ${repo_root}"
go test ./...

echo "Building ${binary_path}"
mkdir -p "${stage_dir}/bin"
go build -trimpath \
  -ldflags "-s -w -X main.version=${version} -X main.commit=${commit} -X main.builtAt=${built_at}" \
  -o "${stage_dir}/bin/${binary_name}" "${binary_path}"

version_output="$("${stage_dir}/bin/${binary_name}" --version)"
case "${version_output}" in
  *"${service}"*"${commit}"*) ;;
  *)
    echo "--version output did not include service ${service} and commit ${commit}: ${version_output}" >&2
    exit 1
    ;;
esac

cat > "${stage_dir}/build-info.json" <<INFO
{"service":"${service}","commit":"${commit}","built_at":"${built_at}","binary_path":"${binary_path}"}
INFO

echo "Installing ${service} ${commit}"
if [[ ! -d "${service_root}" ]]; then
  echo "${service_root} does not exist; create it owned by agent:agents before deploying ${service}" >&2
  exit 1
fi
if [[ ! -w "${service_root}" ]]; then
  echo "${service_root} is not writable by $(id -un); fix ownership before deploying ${service}" >&2
  exit 1
fi

install -d -m 0755 \
  "${service_root}/bin" \
  "${service_root}/releases" \
  "${service_root}/config" \
  "${service_root}/data" \
  "${service_root}/logs" \
  "${service_root}/tmp" \
  "${service_root}/backups"

release_dir="${service_root}/releases/${built_at//[:-]/}"
install -d -m 0755 "${release_dir}"
install -m 0755 "${stage_dir}/bin/${binary_name}" "${release_dir}/${binary_name}"
install -m 0644 "${stage_dir}/build-info.json" "${release_dir}/build-info.json"

if [[ -x "${service_root}/bin/${binary_name}" ]]; then
  install -m 0755 "${service_root}/bin/${binary_name}" "${service_root}/bin/${binary_name}.previous"
fi
install -m 0755 "${stage_dir}/bin/${binary_name}" "${service_root}/bin/${binary_name}"

if [[ -f "${config_example}" && ! -f "${service_root}/config/config.yaml" ]]; then
  install -m 0644 "${config_example}" "${service_root}/config/config.yaml"
fi
if [[ "${service}" == "visual-inspect" ]]; then
  install -d -m 0755 "${service_root}/prompts" "${service_root}/schemas"
  install -m 0644 visual-inspect/prompts/*.md "${service_root}/prompts/"
  install -m 0644 visual-inspect/schemas/*.json "${service_root}/schemas/"
fi
if [[ "${service}" == "gateway" && -f gateway/config/routes.example.yaml && ! -f "${service_root}/config/routes.yaml" ]]; then
  install -m 0644 gateway/config/routes.example.yaml "${service_root}/config/routes.yaml"
fi
if [[ "${service}" == "mcp" && -f mcp/routes.example.yaml ]]; then
  install_mcp_routes
fi

if [[ ! -f "/etc/den-services/${service}.env" ]]; then
  echo "warning: /etc/den-services/${service}.env is missing; create it from ${env_example}" >&2
fi

run_systemctl /bin/systemctl daemon-reload
run_systemctl /bin/systemctl restart "${unit}"

health_response="$(retry_curl "${health_url}")" || {
  rollback
  exit 1
}
version_response="$(retry_curl "${version_url}")" || {
  rollback
  exit 1
}
reported_commit="$(printf '%s' "${version_response}" | json_field commit)"
if [[ "${reported_commit}" != "${commit}" ]]; then
  echo "/version reported commit ${reported_commit}, want ${commit}" >&2
  echo "${version_response}" >&2
  rollback
  exit 1
fi

echo "Installed ${unit}"
echo "Health: ${health_response}"
echo "Version: ${version_response}"
