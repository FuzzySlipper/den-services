#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
installer="${script_dir}/install-codex-playtester.sh"
test_root="$(mktemp -d /tmp/den-playtester-install-test.XXXXXX)"
trap 'rm -rf "${test_root}"' EXIT

assert_refused_unchanged() {
  local name="$1"
  local relative_path="$2"
  local home="${test_root}/${name}"
  local target="${home}/${relative_path}"
  local before="${test_root}/${name}.before"

  mkdir -p "$(dirname "${target}")"
  printf 'unrelated sentinel for %s\n' "${name}" > "${target}"
  cp "${target}" "${before}"

  if "${installer}" --codex-home "${home}" > "${test_root}/${name}.stdout" 2> "${test_root}/${name}.stderr"; then
    echo "installer unexpectedly replaced unrelated ${name}" >&2
    exit 1
  fi
  cmp "${before}" "${target}"
  [[ "$(find "${home}" -type f -o -type l | wc -l)" -eq 1 ]] || {
    echo "installer mutated another target while refusing ${name}" >&2
    exit 1
  }
}

assert_refused_unchanged agent agents/playtester.toml
assert_refused_unchanged configuration playtester/config.yaml
assert_refused_unchanged binary bin/den-playwright

owned_home="${test_root}/owned"
"${installer}" --codex-home "${owned_home}"
"${installer}" --codex-home "${owned_home}"
"${installer}" --check --codex-home "${owned_home}"

grep -Fqx '# Managed by den-services: scripts/install-codex-playtester.sh' \
  "${owned_home}/agents/playtester.toml"
grep -Fqx '# Managed by den-services: scripts/install-codex-playtester.sh' \
  "${owned_home}/playtester/config.yaml"
grep -Fqx 'den-services-codex-playtester-v1' \
  "${owned_home}/playtester/install-owner"

reference_home="${test_root}/reference"
mkdir -p "${reference_home}"
printf '%s\n' \
  '[mcp_servers.den]' \
  'url = "http://127.0.0.1:5199/mcp?tool_profile=planner"' \
  > "${reference_home}/config.toml"
"${installer}" --codex-home "${reference_home}"
"${installer}" --check --codex-home "${reference_home}"
python3 - "${reference_home}/agents/playtester.toml" <<'PY'
import pathlib
import sys
import tomllib

agent = tomllib.loads(pathlib.Path(sys.argv[1]).read_text())
server = agent["mcp_servers"]["den_reference"]
assert server["url"] == "http://127.0.0.1:5199/mcp?tool_profile=planner"
assert set(server["enabled_tools"]) == {
    "den_knowledge_get", "den_knowledge_guide", "den_knowledge_search", "get_document",
}
assert "den_knowledge_store" not in server["enabled_tools"]
PY

printf 'unrelated replacement binary\n' > "${test_root}/replacement-binary"
chmod +x "${test_root}/replacement-binary"
mv "${test_root}/replacement-binary" "${owned_home}/bin/den-playwright"
cp "${owned_home}/bin/den-playwright" "${test_root}/replacement-binary.before"
if "${installer}" --codex-home "${owned_home}" \
  > "${test_root}/replacement-binary.stdout" \
  2> "${test_root}/replacement-binary.stderr"; then
  echo "installer unexpectedly replaced binary after ownership record became stale" >&2
  exit 1
fi
cmp "${test_root}/replacement-binary.before" "${owned_home}/bin/den-playwright"

echo "playtester installer ownership tests passed"
