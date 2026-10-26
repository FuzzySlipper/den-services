#!/usr/bin/env bash
# Deploy den-services to its host from an agent machine.
#
#   scripts/deploy.sh [--commit SHA] [--repo PATH] [--all] [--dry-run]
#
# Pushes the commit (default: origin/main) over SSH into the host checkout,
# then runs that commit's scripts/deploy-host.sh there: fast-forward, rebuild
# and apply migrations, and redeploy only the services whose code changed
# since the commit they are running. Afterwards den-tool on this machine is
# rebuilt when its catalog changed.
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

host_script="/tmp/den-services-deploy-host.$$.sh"
git -C "${repo}" show "${commit}:scripts/deploy-host.sh" | remote "cat > ${host_script}"
set +e
output="$(remote "bash ${host_script} ${commit} --repo ${host_repo} ${host_args[*]:-} </dev/null; status=\$?; rm -f ${host_script}; exit \$status" 2>&1)"
status=$?
set -e
printf '%s\n' "${output}"
[[ ${status} -eq 0 ]] || { echo "deploy: host step failed (exit ${status})" >&2; exit "${status}"; }

# den-tool embeds the MCP catalog; rebuild it here when that changed.
previous_head="$(sed -n 's/^previous_head=//p' <<<"${output}")"
if [[ " ${host_args[*]:-} " != *" --dry-run "* && -n "${previous_head}" ]] &&
  [[ -n "$(git -C "${repo}" diff --name-only "${previous_head}" "${commit}" -- cmd/den-tool mcp/internal/registry mcp/routes.example.yaml)" ]]; then
  install -d "${HOME}/.local/bin"
  tmp="$(mktemp "${HOME}/.local/bin/.den-tool.XXXXXX")"
  remote "cd ${host_repo} && GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory GIT_CONFIG_VALUE_0=${host_repo} go build -o /tmp/den-tool.$$ ./cmd/den-tool && cat /tmp/den-tool.$$ && rm -f /tmp/den-tool.$$" >"${tmp}"
  chmod 0755 "${tmp}"
  "${tmp}" --version >/dev/null && mv "${tmp}" "${HOME}/.local/bin/den-tool"
  echo "den-tool rebuilt for the new MCP catalog"
fi
