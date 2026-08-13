#!/usr/bin/env bash

# Shared, side-effect-light safety helpers for Den deployment scripts.
# Callers keep ownership of sudo/systemd policy and service-specific rollout.

den_env_has_assignment() {
  local env_file="$1"
  local variable="$2"
  if [[ -r "${env_file}" ]]; then
    grep -Eq "^[[:space:]]*${variable}=" "${env_file}"
    return
  fi
  command -v sudo >/dev/null 2>&1 &&
    sudo -n grep -Eq "^[[:space:]]*${variable}=" "${env_file}"
}

den_require_env_assignment() {
  local env_file="$1"
  local variable="$2"
  den_env_has_assignment "${env_file}" "${variable}" || {
    echo "missing required ${variable} in ${env_file}" >&2
    return 1
  }
}

den_mcp_backend_configured() {
  local config_file="$1"
  local backend="$2"
  [[ -r "${config_file}" ]] && awk -v wanted="${backend}" '
    /^[[:space:]]*-[[:space:]]+name:[[:space:]]*/ {
      value=$0
      sub(/^[^:]*:[[:space:]]*/, "", value)
      gsub(/["'\''[:space:]]/, "", value)
      if (value == wanted) found=1
    }
    END { exit(found ? 0 : 1) }
  ' "${config_file}"
}

den_unique_backup() {
  local source="$1"
  local backup_dir="$2"
  local label="$3"
  local backup
  mkdir -p "${backup_dir}"
  backup="$(mktemp "${backup_dir}/${label}.XXXXXXXX")"
  install -m 0644 "${source}" "${backup}"
  printf '%s\n' "${backup}"
}

den_snapshot_file() {
  local source="$1"
  local snapshot="$2"
  if [[ -f "${source}" ]]; then
    install -D -m 0644 "${source}" "${snapshot}"
  else
    install -D -m 0644 /dev/null "${snapshot}.absent"
  fi
}

den_restore_snapshot() {
  local snapshot="$1"
  local target="$2"
  if [[ -f "${snapshot}.absent" ]]; then
    rm -f "${target}"
  elif [[ -f "${snapshot}" ]]; then
    install -m 0644 "${snapshot}" "${target}"
  fi
}
