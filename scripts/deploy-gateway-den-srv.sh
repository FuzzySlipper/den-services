#!/usr/bin/env bash
set -euo pipefail

service="gateway"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
build_only="false"
install_from=""

usage() {
  cat <<'USAGE'
Usage:
  scripts/deploy-gateway-den-srv.sh --build-only
  sudo scripts/deploy-gateway-den-srv.sh --install-from /tmp/gateway-publish.xxxxxx

Build-only creates a staged artifact directory and prints the install command.
Install validates the staged artifacts, installs to /data/services/gateway,
restarts den-go@gateway.service, smokes /health, and prints rollback commands.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --build-only)
      build_only="true"
      shift
      ;;
    --install-from)
      install_from="${2:-}"
      if [[ -z "${install_from}" ]]; then
        echo "--install-from requires a path" >&2
        exit 2
      fi
      shift 2
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

if [[ "${build_only}" == "true" && -n "${install_from}" ]]; then
  echo "--build-only and --install-from are mutually exclusive" >&2
  exit 2
fi

if [[ "${build_only}" == "false" && -z "${install_from}" ]]; then
  build_only="true"
fi

build_gateway() {
  cd "${repo_root}"
  local commit
  local built_at
  local stage_dir
  commit="$(git rev-parse --short=12 HEAD)"
  built_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  stage_dir="$(mktemp -d "/tmp/${service}-publish.XXXXXX")"

  make test
  mkdir -p "${stage_dir}/bin" "${stage_dir}/config" "${stage_dir}/systemd" "${stage_dir}/env"
  go build -trimpath \
    -ldflags "-s -w -X main.version=0.1.0 -X main.commit=${commit} -X main.builtAt=${built_at}" \
    -o "${stage_dir}/bin/${service}" ./gateway/cmd/proxy
  cp gateway/config/config.example.yaml "${stage_dir}/config/config.yaml"
  cp gateway/config/routes.example.yaml "${stage_dir}/config/routes.yaml"
  cp gateway/config/gateway.env.example "${stage_dir}/env/gateway.env.example"
  cat > "${stage_dir}/systemd/den-go@.service.example" <<'UNIT'
[Unit]
Description=Den Go service - %i
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=agent
Group=agents
WorkingDirectory=/data/services/%i
EnvironmentFile=-/etc/den-services/%i.env
Environment=SERVICE_NAME=%i
Environment=SERVICE_ROOT=/data/services/%i
ExecStart=/data/services/%i/bin/%i
Restart=on-failure
RestartSec=5
StartLimitIntervalSec=0
KillMode=mixed
KillSignal=SIGTERM
TimeoutStopSec=30
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=/data/services/%i
StandardOutput=journal
StandardError=journal
SyslogIdentifier=%i

[Install]
WantedBy=multi-user.target
UNIT
  cat > "${stage_dir}/build-info.json" <<INFO
{"service":"${service}","commit":"${commit}","built_at":"${built_at}"}
INFO

  echo "Staged gateway artifacts in: ${stage_dir}"
  echo "Install with:"
  echo "  sudo ${repo_root}/scripts/deploy-gateway-den-srv.sh --install-from ${stage_dir}"
}

install_gateway() {
  local stage_dir="$1"
  local root="/data/services/${service}"
  local unit="den-go@${service}.service"
  local smoke_url="${GATEWAY_SMOKE_URL:-http://127.0.0.1:8079/health}"

  if [[ "${EUID}" -ne 0 ]]; then
    echo "--install-from must be run as root" >&2
    exit 1
  fi
  if [[ ! -x "${stage_dir}/bin/${service}" ]]; then
    echo "missing staged binary: ${stage_dir}/bin/${service}" >&2
    exit 1
  fi

  install -d -m 0755 "${root}/bin" "${root}/releases" "${root}/config" "${root}/data" "${root}/logs" "${root}/tmp" "${root}/backups"
  local release_dir="${root}/releases/$(date -u +%Y%m%dT%H%M%SZ)"
  install -d -m 0755 "${release_dir}"
  install -m 0755 "${stage_dir}/bin/${service}" "${release_dir}/${service}"
  if [[ -f "${stage_dir}/build-info.json" ]]; then
    install -m 0644 "${stage_dir}/build-info.json" "${release_dir}/build-info.json"
  fi
  if [[ -f "${root}/bin/${service}" ]]; then
    install -m 0755 "${root}/bin/${service}" "${root}/bin/${service}.previous"
  fi
  install -m 0755 "${stage_dir}/bin/${service}" "${root}/bin/${service}"
  if [[ ! -f "${root}/config/config.yaml" ]]; then
    install -m 0644 "${stage_dir}/config/config.yaml" "${root}/config/config.yaml"
  fi
  if [[ ! -f "${root}/config/routes.yaml" ]]; then
    install -m 0644 "${stage_dir}/config/routes.yaml" "${root}/config/routes.yaml"
  fi
  if [[ ! -f /etc/systemd/system/den-go@.service && -f "${stage_dir}/systemd/den-go@.service.example" ]]; then
    install -m 0644 "${stage_dir}/systemd/den-go@.service.example" /etc/systemd/system/den-go@.service
  fi
  if [[ ! -f "/etc/den-services/${service}.env" ]]; then
    echo "warning: /etc/den-services/${service}.env is missing; create it from ${stage_dir}/env/gateway.env.example" >&2
  fi

  systemctl daemon-reload
  systemctl restart "${unit}"
  curl -fsS "${smoke_url}" >/dev/null

  echo "Installed and smoked ${unit}"
  echo "Rollback:"
  echo "  sudo systemctl stop ${unit}"
  echo "  sudo install -D -m 0755 ${root}/bin/${service}.previous ${root}/bin/${service}"
  echo "  sudo systemctl start ${unit}"
  echo "  curl -fsS ${smoke_url}"
}

if [[ "${build_only}" == "true" ]]; then
  build_gateway
else
  install_gateway "${install_from}"
fi
