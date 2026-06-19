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

run_root() {
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
  if run_root test -x "${service_root}/bin/${binary_name}.previous"; then
    echo "Smoke failed; rolling back ${unit}" >&2
    run_root install -m 0755 "${service_root}/bin/${binary_name}.previous" "${service_root}/bin/${binary_name}"
    run_root systemctl restart "${unit}"
  fi
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
run_root install -d -m 0755 \
  "${service_root}/bin" \
  "${service_root}/releases" \
  "${service_root}/config" \
  "${service_root}/data" \
  "${service_root}/logs" \
  "${service_root}/tmp" \
  "${service_root}/backups"

release_dir="${service_root}/releases/${built_at//[:-]/}"
run_root install -d -m 0755 "${release_dir}"
run_root install -m 0755 "${stage_dir}/bin/${binary_name}" "${release_dir}/${binary_name}"
run_root install -m 0644 "${stage_dir}/build-info.json" "${release_dir}/build-info.json"

if run_root test -x "${service_root}/bin/${binary_name}"; then
  run_root install -m 0755 "${service_root}/bin/${binary_name}" "${service_root}/bin/${binary_name}.previous"
fi
run_root install -m 0755 "${stage_dir}/bin/${binary_name}" "${service_root}/bin/${binary_name}"

if [[ -f "${config_example}" ]] && ! run_root test -f "${service_root}/config/config.yaml"; then
  run_root install -m 0644 "${config_example}" "${service_root}/config/config.yaml"
fi
if [[ "${service}" == "gateway" && -f gateway/config/routes.example.yaml ]] && ! run_root test -f "${service_root}/config/routes.yaml"; then
  run_root install -m 0644 gateway/config/routes.example.yaml "${service_root}/config/routes.yaml"
fi

if ! run_root test -f "/etc/den-services/${service}.env"; then
  echo "warning: /etc/den-services/${service}.env is missing; create it from ${env_example}" >&2
fi

run_root systemctl daemon-reload
run_root systemctl restart "${unit}"

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
