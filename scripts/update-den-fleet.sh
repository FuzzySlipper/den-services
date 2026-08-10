#!/usr/bin/env bash
# Update the deployed Den service fleet over SSH.
#
# This is an orchestrator, not a second deployment implementation. Den Go
# services are updated through scripts/den-services-deploy.sh, the Den Web
# assets through den-web's release publisher, and the native Rusty Crew/View
# pair through Rusty View's host updater.
set -Eeuo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/update-den-fleet.sh --target m5|lore|all [--preflight]

Updates the active den-services Go units, the Den Web frontend release, and the
configured Rusty Crew/View deployment on the selected remote machine. With
--preflight it only checks SSH, paths, repository cleanliness, sudo, active
units, and deployment shape.

Configuration is an optional non-secret shell fragment, for example:

  DEN_FLEET_CONFIG=$HOME/.config/den-fleet.conf \
    scripts/update-den-fleet.sh --target m5

Run --preflight for Lore first. Lore's SSH account and Rusty deployment mode
must be configured before an update is allowed there.
USAGE
}

target=""
preflight_only="false"
config_path="${DEN_FLEET_CONFIG:-${HOME:-}/.config/den-fleet.conf}"
if [[ -n "${DEN_FLEET_CONFIG:-}" && ! -r "${DEN_FLEET_CONFIG}" ]]; then
  echo "DEN_FLEET_CONFIG is not readable: ${DEN_FLEET_CONFIG}" >&2
  exit 2
fi
if [[ -r "${config_path}" ]]; then
  # shellcheck disable=SC1090
  source "${config_path}"
fi

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target)
      target="${2:-}"
      shift 2
      ;;
    --preflight)
      preflight_only="true"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

case "${target}" in
  m5|lore|all) ;;
  *)
    echo "--target must be m5, lore, or all" >&2
    usage >&2
    exit 2
    ;;
esac

nephew_ssh_config="${DEN_FLEET_NEPHEW_SSH_CONFIG:-${HOME:-}/.ssh/nephew-agentbox.conf}"
nephew_ssh_target="${DEN_FLEET_NEPHEW_SSH_TARGET:-nephew-agentbox}"
nephew_den_repo="${DEN_FLEET_NEPHEW_DEN_REPO:-/data/services/den-services}"
nephew_den_web_repo="${DEN_FLEET_NEPHEW_DEN_WEB_REPO:-/data/dev/den-web}"
nephew_den_web_root="${DEN_FLEET_NEPHEW_DEN_WEB_ROOT:-/data/services/den-web}"
nephew_den_web_url="${DEN_FLEET_NEPHEW_DEN_WEB_URL:-http://127.0.0.1:18080}"
nephew_den_web_skip_checks="${DEN_FLEET_NEPHEW_DEN_WEB_SKIP_CHECKS:-1}"
nephew_den_web_skip_install="${DEN_FLEET_NEPHEW_DEN_WEB_SKIP_INSTALL:-0}"
nephew_rusty_updater="${DEN_FLEET_NEPHEW_RUSTY_UPDATER:-/usr/local/sbin/update-rusty-stack}"
nephew_rusty_root="${DEN_FLEET_NEPHEW_RUSTY_ROOT:-/data/services/rusty-stack}"

# Do not invent a Lore account or deployment root. These defaults describe
# the known Den root and the m5-style updater only; preflight reports what Lore
# actually has, and the operator can set these after SSH access is established.
lore_ssh_config="${DEN_FLEET_LORE_SSH_CONFIG:-${HOME:-}/.ssh/config}"
lore_ssh_target="${DEN_FLEET_LORE_SSH_TARGET:-lore-desktop}"
lore_ssh_identity="${DEN_FLEET_LORE_SSH_IDENTITY_FILE:-}"
lore_den_repo="${DEN_FLEET_LORE_DEN_REPO:-/data/services/den-services}"
lore_den_web_repo="${DEN_FLEET_LORE_DEN_WEB_REPO:-/data/dev/den-web}"
lore_den_web_root="${DEN_FLEET_LORE_DEN_WEB_ROOT:-/data/services/den-web}"
lore_den_web_url="${DEN_FLEET_LORE_DEN_WEB_URL:-http://127.0.0.1:18080}"
lore_den_web_skip_checks="${DEN_FLEET_LORE_DEN_WEB_SKIP_CHECKS:-1}"
lore_den_web_skip_install="${DEN_FLEET_LORE_DEN_WEB_SKIP_INSTALL:-0}"
lore_rusty_updater="${DEN_FLEET_LORE_RUSTY_UPDATER:-/data/services/rusty-crew/bin/update-rusty-stack}"
lore_rusty_root="${DEN_FLEET_LORE_RUSTY_ROOT:-/data/services/rusty-crew}"

die() {
  echo "update-den-fleet: $*" >&2
  exit 1
}

ssh_args_for() {
  local machine="$1"
  case "${machine}" in
    m5)
      [[ -r "${nephew_ssh_config}" ]] || die "m5 SSH config is missing: ${nephew_ssh_config}"
      printf '%s\n' \
        "-F" "${nephew_ssh_config}" \
        "-o" "BatchMode=yes" \
        "-o" "ControlMaster=no" \
        "-o" "ControlPath=none" \
        "-o" "PreferredAuthentications=publickey" \
        "-o" "StrictHostKeyChecking=accept-new" \
        "${nephew_ssh_target}"
      ;;
    lore)
      [[ -r "${lore_ssh_config}" ]] || die "Lore SSH config is missing: ${lore_ssh_config}"
      if [[ -n "${lore_ssh_identity}" ]]; then
        printf '%s\n' \
          "-F" "${lore_ssh_config}" \
          "-o" "BatchMode=yes" \
          "-o" "ControlMaster=no" \
          "-o" "ControlPath=none" \
          "-o" "StrictHostKeyChecking=accept-new" \
          "-o" "IdentityFile=${lore_ssh_identity}" \
          "-o" "IdentitiesOnly=yes" \
          "${lore_ssh_target}"
      else
        printf '%s\n' \
          "-F" "${lore_ssh_config}" \
          "-o" "BatchMode=yes" \
          "-o" "ControlMaster=no" \
          "-o" "ControlPath=none" \
          "-o" "StrictHostKeyChecking=accept-new" \
          "${lore_ssh_target}"
      fi
      ;;
    *) die "unknown machine ${machine}" ;;
  esac
}

remote_script() {
  local machine="$1"
  local mode="$2"
  local den_repo="$3"
  local den_web_repo="$4"
  local den_web_root="$5"
  local den_web_url="$6"
  local den_web_skip_checks="$7"
  local den_web_skip_install="$8"
  local rusty_updater="$9"
  local rusty_root="${10}"
  local rusty_as_root="${11}"
  local -a ssh_args
  mapfile -t ssh_args < <(ssh_args_for "${machine}")

  echo "== ${machine}: ${mode} =="
  if ! ssh "${ssh_args[@]}" bash -s -- \
    "${mode}" "${den_repo}" "${den_web_repo}" "${den_web_root}" \
    "${den_web_url}" "${den_web_skip_checks}" "${den_web_skip_install}" \
    "${rusty_updater}" "${rusty_root}" "${rusty_as_root}" <<'REMOTE'
set -Eeuo pipefail

mode="$1"
den_repo="$2"
den_web_repo="$3"
den_web_root="$4"
den_web_url="$5"
den_web_skip_checks="$6"
den_web_skip_install="$7"
rusty_updater="$8"
rusty_root="$9"
rusty_as_root="${10}"

log() {
  printf '[remote] %s\n' "$*"
}

fail() {
  printf '[remote] ERROR: %s\n' "$*" >&2
  exit 1
}

[[ -d "${den_repo}/.git" ]] || fail "den-services checkout is missing: ${den_repo}"
[[ -f "${den_repo}/scripts/den-services-deploy.sh" ]] ||
  fail "den-services deploy script is missing under ${den_repo}"
[[ -d "${den_web_repo}/.git" ]] || fail "den-web checkout is missing: ${den_web_repo}"
[[ -f "${den_web_repo}/package.json" ]] || fail "den-web package.json is missing: ${den_web_repo}"
[[ -d "${den_web_root}" ]] || fail "den-web deploy root is missing: ${den_web_root}"
command -v node >/dev/null 2>&1 || fail "node is required for the den-web deployment"
command -v npm >/dev/null 2>&1 || fail "npm is required for the den-web deployment"

if [[ -n "$(git -C "${den_repo}" status --porcelain)" ]]; then
  git -C "${den_repo}" status --short >&2
  fail "refusing a dirty den-services checkout"
fi
if [[ -n "$(git -C "${den_web_repo}" status --porcelain)" ]]; then
  git -C "${den_web_repo}" status --short >&2
  fail "refusing a dirty den-web checkout"
fi

if ! sudo -n /bin/systemctl daemon-reload >/dev/null 2>&1; then
  fail "passwordless sudo for systemctl is required for Den service restarts"
fi

active_units="$(systemctl list-units --type=service --state=active \
  'den-go@*.service' --no-legend --plain | awk '{print $1}' | sort)"
if [[ -z "${active_units}" ]]; then
  fail "no active den-go units were found"
fi

registry_names="$(python3 - "${den_repo}/deployment/services.yaml" <<'PY'
import sys
from pathlib import Path

for line in Path(sys.argv[1]).read_text(encoding="utf-8").splitlines():
    stripped = line.strip()
    if stripped.startswith('- name:'):
        value = stripped.split(':', 1)[1].strip()
        print(value.strip("\\\"'"))
PY
)"

active_services=()
while IFS= read -r unit; do
  [[ -n "${unit}" ]] || continue
  service="${unit#den-go@}"
  service="${service%.service}"
  if ! grep -Fxq "${service}" <<<"${registry_names}"; then
    fail "active unit ${unit} is not registered in deployment/services.yaml"
  fi
  active_services+=("${service}")
done <<<"${active_units}"

gateway_env=/etc/den-services/gateway.env
mcp_env=/etc/den-services/mcp.env
mcp_config=/data/services/mcp/config/config.yaml
grep -Eq '^DEN_GATEWAY_KNOWLEDGE_UPSTREAM_TOKEN=' "${gateway_env}" ||
  fail "${gateway_env} lacks DEN_GATEWAY_KNOWLEDGE_UPSTREAM_TOKEN; complete the Knowledge route enrollment first"
systemctl is-active --quiet den-go@handoff.service ||
  fail "den-go@handoff.service is not active; enroll the registered Handoff successor before updating MCP"
grep -Eq '^DEN_HANDOFF_SERVICE_TOKEN=' "${mcp_env}" ||
  fail "${mcp_env} lacks DEN_HANDOFF_SERVICE_TOKEN"
awk '
  /^[[:space:]]*-[[:space:]]+name:[[:space:]]*/ {
    value=$0
    sub(/^[^:]*:[[:space:]]*/, "", value)
    gsub(/["'\''[:space:]]/, "", value)
    if (value == "handoff") found=1
  }
  END { exit(found ? 0 : 1) }
' "${mcp_config}" || fail "${mcp_config} lacks the handoff backend"

legacy_web_active="false"
if systemctl is-active --quiet den-web.service 2>/dev/null; then
  legacy_web_active="true"
fi
web_edge_active="false"
if systemctl is-active --quiet den-go@web-edge.service 2>/dev/null; then
  web_edge_active="true"
fi
if [[ "${legacy_web_active}" == "true" && "${web_edge_active}" != "true" ]]; then
  fail "legacy den-web.service still owns port 18080; migrate it to den-go@web-edge.service before updating"
fi
if [[ "${web_edge_active}" != "true" ]]; then
  fail "den-go@web-edge.service is not active"
fi

if [[ "${mode}" == "preflight" ]]; then
  log "den checkout $(git -C "${den_repo}" rev-parse HEAD)"
  log "den-web checkout $(git -C "${den_web_repo}" rev-parse HEAD)"
  log "den-web release root: ${den_web_root}"
  log "web-edge: active on ${den_web_url}"
  log "active services: ${active_services[*]}"
else
  old_den_sha="$(git -C "${den_repo}" rev-parse HEAD)"
  log "fetching den-services main (from ${old_den_sha:0:12})"
  git -C "${den_repo}" fetch --prune origin main
  git -C "${den_repo}" checkout --detach origin/main
  new_den_sha="$(git -C "${den_repo}" rev-parse HEAD)"
  log "den-services source is ${new_den_sha}"

  # This order follows the documented dependency waves. Only units that are
  # active on this host are restarted; optional/omitted registry entries stay
  # untouched.
  deployment_order=(
    projects knowledge artifacts runtime conversation
    tasks documents delivery
    messages guidance observation timeline
    review librarian handoff
    visual-contract visual-inspect doc-publish
    gateway web-edge mcp
  )
  is_active_service() {
    local wanted="$1"
    local active
    for active in "${active_services[@]}"; do
      [[ "${active}" == "${wanted}" ]] && return 0
    done
    return 1
  }
  deployed=()
  for service in "${deployment_order[@]}" "${active_services[@]}"; do
    already_deployed="false"
    for previous in "${deployed[@]}"; do
      [[ "${previous}" == "${service}" ]] && already_deployed="true"
    done
    [[ "${already_deployed}" == "true" ]] && continue
    if is_active_service "${service}"; then
      log "deploying den-go@${service}.service"
      "${den_repo}/scripts/den-services-deploy.sh" "${service}" \
        --repo "${den_repo}" --no-pull
      deployed+=("${service}")
    fi
  done

  old_web_sha="$(git -C "${den_web_repo}" rev-parse HEAD)"
  log "fetching den-web main (from ${old_web_sha:0:12})"
  git -C "${den_web_repo}" fetch --prune origin main
  git -C "${den_web_repo}" checkout --detach origin/main
  new_web_sha="$(git -C "${den_web_repo}" rev-parse HEAD)"
  log "den-web source is ${new_web_sha}"
  log "publishing den-web through web-edge"
  (
    cd "${den_web_repo}"
    deploy_env=(
      "NX_DAEMON=false"
      "NX_TUI=false"
      "DEN_WEB_URL=${den_web_url}"
      "DEPLOY_ROOT=${den_web_root}"
      "ENVIRONMENT_NAME=$(hostname -s)"
    )
    [[ "${den_web_skip_checks}" == "1" ]] && deploy_env+=("SKIP_CHECKS=1")
    [[ "${den_web_skip_install}" == "1" ]] && deploy_env+=("SKIP_INSTALL=1")
    env "${deploy_env[@]}" npm run deploy:den-srv
  )
fi

rusty_shape="missing"
if [[ -x "${rusty_updater}" ]]; then
  rusty_shape="native"
elif [[ -f "${rusty_root}/compose.yaml" ]]; then
  rusty_shape="legacy-compose"
fi

case "${rusty_shape}" in
  native)
    if [[ "${mode}" == "preflight" ]]; then
      log "Rusty deployment: native updater ${rusty_updater}"
      log "Rusty root: ${rusty_root}"
    else
      log "running ${rusty_updater}"
      if [[ "${rusty_as_root}" == "true" ]]; then
        sudo -n "${rusty_updater}"
      else
        "${rusty_updater}"
      fi
    fi
    ;;
  legacy-compose)
    fail "Rusty deployment is legacy Compose at ${rusty_root}; configure a reviewed Lore updater before mutating it"
    ;;
  missing)
    fail "Rusty updater is not present at ${rusty_updater}; configure the target's deployment path first"
    ;;
esac

if [[ "${mode}" != "preflight" ]]; then
  failed_units="$(systemctl list-units --type=service --state=failed --no-legend --plain \
    | awk '$1 ~ /^(den-go@|rusty-crew-m5@)/' || true)"
  [[ -z "${failed_units}" ]] || fail "failed systemd units after update:\n${failed_units}"
  log "MCP version: $(curl -fsS http://127.0.0.1:5199/version)"
  log "Rusty View A health: $(curl -fsS http://127.0.0.1:9347/v1/admin/healthz)"
  log "Den Web build: $(curl -fsS "${den_web_url}/den-web-build.json")"
fi
REMOTE
  then
    die "SSH/remote preflight failed for ${machine}"
  fi
}

preflight_machine() {
  local machine="$1"
  case "${machine}" in
    m5)
      remote_script "m5" "preflight" "${nephew_den_repo}" \
        "${nephew_den_web_repo}" "${nephew_den_web_root}" "${nephew_den_web_url}" \
        "${nephew_den_web_skip_checks}" "${nephew_den_web_skip_install}" \
        "${nephew_rusty_updater}" "${nephew_rusty_root}" "true"
      ;;
    lore)
      remote_script "lore" "preflight" "${lore_den_repo}" \
        "${lore_den_web_repo}" "${lore_den_web_root}" "${lore_den_web_url}" \
        "${lore_den_web_skip_checks}" "${lore_den_web_skip_install}" \
        "${lore_rusty_updater}" "${lore_rusty_root}" "false"
      ;;
  esac
}

update_machine() {
  local machine="$1"
  case "${machine}" in
    m5)
      remote_script "m5" "update" "${nephew_den_repo}" \
        "${nephew_den_web_repo}" "${nephew_den_web_root}" "${nephew_den_web_url}" \
        "${nephew_den_web_skip_checks}" "${nephew_den_web_skip_install}" \
        "${nephew_rusty_updater}" "${nephew_rusty_root}" "true"
      ;;
    lore)
      remote_script "lore" "update" "${lore_den_repo}" \
        "${lore_den_web_repo}" "${lore_den_web_root}" "${lore_den_web_url}" \
        "${lore_den_web_skip_checks}" "${lore_den_web_skip_install}" \
        "${lore_rusty_updater}" "${lore_rusty_root}" "false"
      ;;
  esac
}

machines=()
case "${target}" in
  m5) machines=(m5) ;;
  lore) machines=(lore) ;;
  all) machines=(m5 lore) ;;
esac

if [[ "${preflight_only}" == "true" ]]; then
  for machine in "${machines[@]}"; do
    preflight_machine "${machine}"
  done
  exit 0
fi

# Preflight every selected host before changing any of them. This makes
# --target all fail closed when, for example, Lore still needs an SSH key.
for machine in "${machines[@]}"; do
  preflight_machine "${machine}"
done
for machine in "${machines[@]}"; do
  update_machine "${machine}"
done
