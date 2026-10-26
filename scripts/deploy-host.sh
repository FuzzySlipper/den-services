#!/usr/bin/env bash
# Host half of scripts/deploy.sh. Runs on the Den service host as root with the
# target commit already pushed into the checkout as refs/deploy/target.
#
# It fast-forwards the checkout, rebuilds and applies migrations, then deploys
# every registered service whose running /version commit is behind the
# checkout and whose code changed. Each service goes through
# scripts/den-services-deploy.sh, which builds, tests, installs, restarts,
# checks /health and /version, and rolls back on failure.
set -euo pipefail

usage() {
  echo "usage: deploy-host.sh <commit> [--repo PATH] [--all] [--dry-run]" >&2
}

target="${1:-}"
[[ -n "${target}" ]] || { usage; exit 2; }
shift
repo="/data/services/den-services"
deploy_all=false
dry_run=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo) repo="${2:?--repo requires a path}"; shift 2 ;;
    --all) deploy_all=true; shift ;;
    --dry-run) dry_run=true; shift ;;
    *) usage; exit 2 ;;
  esac
done

die() { echo "deploy-host: $*" >&2; exit 1; }
[[ "${EUID}" -eq 0 ]] || die "run as root"
[[ -d "${repo}/.git" ]] || die "no checkout at ${repo}"

exec 9>/run/den-services-deploy.lock
flock -w 1800 9 || die "another deploy held the lock for 30 minutes"

owner="$(stat -c %U "${repo}")"
export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory GIT_CONFIG_VALUE_0="${repo}"
git_owner() { sudo -u "${owner}" env GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory GIT_CONFIG_VALUE_0="${repo}" git -C "${repo}" "$@"; }
cd "${repo}"
# Git commands run as root here (and in den-services-deploy.sh) can leave
# root-owned files such as .git/index; hand the repository back on exit.
chown -R --reference="${repo}" "${repo}/.git"
trap 'chown -R --reference="${repo}" "${repo}/.git"' EXIT
git_owner worktree prune

target="$(git rev-parse --verify "${target}^{commit}")" || die "unknown commit ${target}"
[[ -z "$(git_owner status --porcelain)" ]] || { git_owner status --short >&2; die "checkout is dirty"; }

previous_head="$(git rev-parse HEAD)"
echo "previous_head=${previous_head}"
if git merge-base --is-ancestor "${target}" HEAD; then
  echo "checkout ${previous_head:0:12} already contains ${target:0:12}"
elif git merge-base --is-ancestor HEAD "${target}"; then
  if [[ "${dry_run}" == "true" ]]; then
    echo "would fast-forward ${previous_head:0:12} -> ${target:0:12}"
  else
    git_owner merge --ff-only --quiet "${target}"
    echo "fast-forwarded ${previous_head:0:12} -> ${target:0:12}"
  fi
else
  die "checkout HEAD ${previous_head:0:12} and ${target:0:12} have diverged"
fi
head="$(git rev-parse HEAD)"
[[ "${dry_run}" == "true" ]] && head="$(git rev-parse "${target}")"
echo "deploy_head=${head}"

# Migrations run first so new service code finds its schema. The runner is
# rebuilt every time because the migration files are embedded in it.
migrate_bin="${repo}/bin/den-services-migrate"
run_migrations() {
  install -d -o "${owner}" -m 0755 "${repo}/bin"
  go build -trimpath -o "${migrate_bin}.new" ./migration/cmd/migrate
  chown "${owner}" "${migrate_bin}.new"
  mv "${migrate_bin}.new" "${migrate_bin}"
  (
    set -a
    # shellcheck disable=SC1091
    . /etc/den-services/postgresql.env
    set +a
    status="$("${migrate_bin}" status)"
    if grep -q 'pending=[1-9]' <<<"${status}"; then
      grep 'pending=[1-9]' <<<"${status}"
      "${migrate_bin}" up
      status="$("${migrate_bin}" status)"
      if grep -q 'pending=[1-9]' <<<"${status}"; then
        echo "${status}" >&2
        exit 1
      fi
      echo "migrations applied"
    else
      echo "migrations: none pending"
    fi
  )
}
if [[ "${dry_run}" == "true" ]]; then
  echo "would rebuild the migration runner and apply pending migrations"
else
  run_migrations || die "migrations failed; no service was deployed"
fi

# One "name module version_url unit" line per registered service.
registered_services() {
  python3 - "${repo}/deployment/services.yaml" <<'PY'
import sys
services, current = [], None
for raw in open(sys.argv[1], encoding="utf-8"):
    line = raw.split("#", 1)[0].rstrip()
    if not line.strip():
        continue
    stripped = line.strip()
    if stripped.startswith("- "):
        if current is not None:
            services.append(current)
        current = {}
        stripped = stripped[2:].strip()
        if not stripped:
            continue
    if current is None or ":" not in stripped:
        continue
    key, _, value = stripped.partition(":")
    current[key.strip()] = value.strip().strip('"').strip("'")
if current is not None:
    services.append(current)
for s in services:
    print(s["name"], s["module"], s["version_url"], s["systemd_unit"])
PY
}

# Paths whose change means every service must be rebuilt.
shared_paths=(shared go.mod go.sum go.work go.work.sum deployment/services.yaml scripts/den-services-deploy.sh scripts/lib)

deployed=() unchanged=() failed=() skipped=()
while read -r name module version_url unit; do
  if ! systemctl is-enabled --quiet "${unit}" 2>/dev/null; then
    skipped+=("${name} (unit ${unit} not enabled; first install is manual)")
    continue
  fi
  running="$(curl -fsS -m 5 "${version_url}" 2>/dev/null | python3 -c 'import json,sys; print(json.load(sys.stdin).get("commit",""))' 2>/dev/null || true)"
  reason=""
  if [[ "${deploy_all}" == "true" ]]; then
    reason="--all"
  elif [[ -z "${running}" ]]; then
    reason="version unavailable"
  elif ! running_full="$(git rev-parse --verify --quiet "${running}^{commit}")"; then
    reason="running commit ${running} unknown to the checkout"
  elif [[ "${running_full}" == "${head}" ]]; then
    unchanged+=("${name}")
    continue
  elif ! git merge-base --is-ancestor "${running_full}" "${head}"; then
    reason="running commit ${running} is not an ancestor of ${head:0:12}"
  elif [[ -n "$(git diff --name-only "${running_full}" "${head}" -- "${module}" "${shared_paths[@]}")" ]]; then
    reason="changed since ${running}"
  else
    unchanged+=("${name}")
    continue
  fi
  if [[ "${dry_run}" == "true" ]]; then
    deployed+=("${name} (would deploy: ${reason})")
    continue
  fi
  echo "== deploying ${name}: ${reason}"
  if scripts/den-services-deploy.sh "${name}" --no-pull --repo "${repo}" </dev/null; then
    deployed+=("${name} (${reason})")
  else
    failed+=("${name}")
  fi
done < <(registered_services)

echo
echo "deployed: ${#deployed[@]}"
if [[ ${#deployed[@]} -gt 0 ]]; then printf '  %s\n' "${deployed[@]}"; fi
echo "unchanged: ${#unchanged[@]} (${unchanged[*]})"
if [[ ${#skipped[@]} -gt 0 ]]; then echo "skipped: ${#skipped[@]}"; printf '  %s\n' "${skipped[@]}"; fi
if [[ ${#failed[@]} -gt 0 ]]; then
  echo "failed (rolled back by den-services-deploy.sh): ${failed[*]}"
  exit 1
fi
