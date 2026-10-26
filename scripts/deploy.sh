#!/usr/bin/env bash
# Deploy den-services to its host from an agent machine.
#
#   scripts/deploy.sh [--commit SHA] [--repo PATH] [--all] [--dry-run]
#
# Pushes the commit (default: origin/main) over SSH into the host checkout,
# then runs that commit's scripts/deploy-host.sh there, detached from the SSH
# session: fast-forward, rebuild and apply migrations, and redeploy only the
# services whose code changed since the commit they are running. Afterwards
# den-tool on this machine is rebuilt when its catalog changed since the commit
# it was last built from.
#
# Deploy only reviewed commits: crew-review runs this after looks_good.
#
# Environment:
#   DEN_DEPLOY_SSH        ssh command for the host
#                         (default: ssh -F ~/server-access/den-services/config target)
#   DEN_DEPLOY_HOST_REPO  checkout path on the host (default: /data/services/den-services)
set -euo pipefail

usage() {
  sed -n '4,5p' "$0" >&2
}

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
commit=""
host_args=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --commit) commit="${2:?--commit requires a SHA}"; shift 2 ;;
    --repo) repo="${2:?--repo requires a path}"; shift 2 ;;
    --all|--dry-run) host_args+=("$1"); shift ;;
    -h|--help) usage; exit 0 ;;
    *) usage; exit 2 ;;
  esac
done

ssh_cmd="${DEN_DEPLOY_SSH:-ssh -F ${HOME}/server-access/den-services/config target}"
host_repo="${DEN_DEPLOY_HOST_REPO:-/data/services/den-services}"
read -r -a ssh_argv <<<"${ssh_cmd}"
ssh_transport=("${ssh_argv[@]:0:${#ssh_argv[@]}-1}")
ssh_host="${ssh_argv[-1]}"
remote() { "${ssh_argv[@]}" "$@"; }

git -C "${repo}" fetch --quiet origin
commit="$(git -C "${repo}" rev-parse --verify "${commit:-origin/main}^{commit}")"
git -C "${repo}" merge-base --is-ancestor "${commit}" origin/main ||
  { echo "deploy: ${commit} is not on origin/main" >&2; exit 1; }
echo "deploy: ${commit:0:12} $(git -C "${repo}" log -1 --format=%s "${commit}")"

# Push as the checkout's owner so new objects keep its ownership.
GIT_SSH_COMMAND="${ssh_transport[*]}" git -C "${repo}" push --quiet \
  --receive-pack="sudo -u \$(stat -c %U ${host_repo}) git-receive-pack" \
  "${ssh_host}:${host_repo}" "+${commit}:refs/deploy/target"

# The host step runs detached from this SSH session, so a dropped connection
# or a stopped caller cannot interrupt a deploy midway; this side waits for it.
run_id="den-services-deploy.$$.$(date +%s)"
host_script="/tmp/${run_id}.sh"
git -C "${repo}" show "${commit}:scripts/deploy-host.sh" | remote "cat > ${host_script}"
remote "setsid bash -c 'bash ${host_script} ${commit} --repo ${host_repo} ${host_args[*]:-} >/tmp/${run_id}.log 2>&1; echo \$? >/tmp/${run_id}.status' </dev/null >/dev/null 2>&1 &"
until remote "test -f /tmp/${run_id}.status"; do sleep 5; done
output="$(remote "cat /tmp/${run_id}.log")"
status="$(remote "cat /tmp/${run_id}.status; rm -f ${host_script} /tmp/${run_id}.status")"
printf '%s\n' "${output}"
[[ "${status}" == "0" ]] || { echo "deploy: host step failed (exit ${status}); log /tmp/${run_id}.log on the host" >&2; exit 1; }
remote "rm -f /tmp/${run_id}.log"
[[ " ${host_args[*]:-} " == *" --dry-run "* ]] && exit 0

# den-tool embeds the MCP catalog. Rebuild it when the catalog changed since
# the commit it was last built from (recorded beside it), or when that is
# unknown, so a failed run followed by a retry still updates it.
den_tool="${HOME}/.local/bin/den-tool"
built_from_file="${den_tool}.built-from"
built_from="$(cat "${built_from_file}" 2>/dev/null || true)"
catalog_paths=(cmd/den-tool mcp/internal/registry mcp/routes.example.yaml)
if [[ -z "${built_from}" ]] || ! git -C "${repo}" cat-file -e "${built_from}^{commit}" 2>/dev/null ||
  [[ -n "$(git -C "${repo}" diff --name-only "${built_from}" "${commit}" -- "${catalog_paths[@]}")" ]]; then
  install -d "${HOME}/.local/bin"
  tmp="$(mktemp "${HOME}/.local/bin/.den-tool.XXXXXX")"
  remote "cd ${host_repo} && GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory GIT_CONFIG_VALUE_0=${host_repo} go build -o /tmp/${run_id}.den-tool ./cmd/den-tool && cat /tmp/${run_id}.den-tool; status=\$?; rm -f /tmp/${run_id}.den-tool; exit \$status" >"${tmp}"
  chmod 0755 "${tmp}"
  "${tmp}" --version >/dev/null
  mv "${tmp}" "${den_tool}"
  printf '%s\n' "${commit}" >"${built_from_file}"
  echo "den-tool rebuilt from ${commit:0:12}"
fi
