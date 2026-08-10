#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/deploy-safety.sh
source "${repo_root}/scripts/lib/deploy-safety.sh"

test_root="$(mktemp -d)"
trap 'rm -rf "${test_root}"' EXIT

printf 'EXISTING=value\n' > "${test_root}/service.env"
den_env_has_assignment "${test_root}/service.env" EXISTING
if den_require_env_assignment "${test_root}/service.env" MISSING 2>/dev/null; then
  echo "missing environment prerequisite was accepted" >&2
  exit 1
fi

cat > "${test_root}/config.yaml" <<'YAML'
backends:
  - name: "knowledge"
YAML
den_mcp_backend_configured "${test_root}/config.yaml" knowledge
if den_mcp_backend_configured "${test_root}/config.yaml" handoff; then
  echo "missing MCP backend was accepted" >&2
  exit 1
fi

printf 'original\n' > "${test_root}/routes.yaml"
first="$(den_unique_backup "${test_root}/routes.yaml" "${test_root}/backups" routes.yaml)"
second="$(den_unique_backup "${test_root}/routes.yaml" "${test_root}/backups" routes.yaml)"
[[ "${first}" != "${second}" && -f "${first}" && -f "${second}" ]]

den_snapshot_file "${test_root}/routes.yaml" "${test_root}/rollback/routes.yaml"
printf 'migrated\n' > "${test_root}/routes.yaml"
den_restore_snapshot "${test_root}/rollback/routes.yaml" "${test_root}/routes.yaml"
grep -Fxq original "${test_root}/routes.yaml"

echo "deploy safety tests passed"
