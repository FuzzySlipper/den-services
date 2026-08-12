#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/install-codex-playtester.sh [--check] [--codex-home PATH]

Install (default): build den-playwright, link the repository-owned skill, render
the Luna/max playtester agent and local MCP config, then validate the result.

Check: validate an existing installation and MCP tool catalog without changing it.
USAGE
}

mode="install"
codex_root="${CODEX_HOME:-${HOME}/.codex}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --check)
      mode="check"
      shift
      ;;
    --codex-home)
      [[ $# -ge 2 ]] || { echo "--codex-home requires a path" >&2; exit 2; }
      codex_root="$2"
      shift 2
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

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
source_skill="${repo_root}/codex/skills/product-playtest"
agent_template="${repo_root}/codex/agents/playtester.toml.template"
config_template="${repo_root}/codex/playtester/config.yaml.template"
installed_skill="${codex_root}/skills/product-playtest"
installed_agent="${codex_root}/agents/playtester.toml"
installed_binary="${codex_root}/bin/den-playwright"
installed_input_helper="${codex_root}/bin/den-playwright-x11-input"
installed_config="${codex_root}/playtester/config.yaml"
state_dir="${codex_root}/playtester/state"
artifact_root="${codex_root}/playtester/runs"
codex_config="${codex_root}/config.toml"
driver_script="${repo_root}/playwright-broker/driver/playtest-driver.mjs"
owner_record="${codex_root}/playtester/install-owner"
owner_marker="# Managed by den-services: scripts/install-codex-playtester.sh"

require_file() {
  [[ -f "$1" ]] || { echo "missing required file: $1" >&2; exit 1; }
}

require_file "${source_skill}/SKILL.md"
require_file "${agent_template}"
require_file "${config_template}"
require_file "${driver_script}"

path_exists() {
  [[ -e "$1" || -L "$1" ]]
}

has_owner_marker() {
  [[ -f "$1" && ! -L "$1" ]] && grep -Fqx "${owner_marker}" "$1"
}

has_owner_record() {
  [[ -f "${owner_record}" && ! -L "${owner_record}" ]] \
    && grep -Fqx "den-services-codex-playtester-v1" "${owner_record}" \
    && grep -Fqx "repo_root=${repo_root}" "${owner_record}"
}

has_owned_binary() {
  [[ -f "${installed_binary}" && ! -L "${installed_binary}" ]] || return 1
  has_owner_record || return 1
  local expected_hash
  expected_hash="$(sed -n 's/^binary_sha256=//p' "${owner_record}")"
  [[ -n "${expected_hash}" ]] || return 1
  [[ "$(sha256sum "${installed_binary}" | awk '{print $1}')" == "${expected_hash}" ]]
}

has_owned_input_helper() {
  [[ -f "${installed_input_helper}" && ! -L "${installed_input_helper}" ]] || return 1
  has_owner_record || return 1
  local expected_hash
  expected_hash="$(sed -n 's/^input_helper_sha256=//p' "${owner_record}")"
  [[ -n "${expected_hash}" ]] || return 1
  [[ "$(sha256sum "${installed_input_helper}" | awk '{print $1}')" == "${expected_hash}" ]]
}

is_legacy_owned_install() {
  [[ -f "${installed_agent}" && -f "${installed_config}" && -x "${installed_binary}" ]] || return 1
  [[ ! -L "${installed_agent}" && ! -L "${installed_config}" && ! -L "${installed_binary}" ]] || return 1
  [[ -L "${installed_skill}" ]] || return 1
  [[ "$(readlink -f "${installed_skill}")" == "$(readlink -f "${source_skill}")" ]] || return 1
  grep -Fqx '# Rendered by scripts/install-codex-playtester.sh.' "${installed_config}" || return 1

  python3 - \
    "${installed_agent}" "${installed_skill}/SKILL.md" "${installed_binary}" \
    "${installed_config}" "${repo_root}" <<'PY'
import pathlib
import sys
import tomllib

agent_path, skill_path, binary_path, config_path, repo_root = map(pathlib.Path, sys.argv[1:])
try:
    agent = tomllib.loads(agent_path.read_text())
    server = agent["mcp_servers"]["den_playtest"]
    skill = agent["skills"]["config"][0]
except (KeyError, IndexError, OSError, tomllib.TOMLDecodeError):
    raise SystemExit(1)

valid = (
    agent.get("name") == "playtester"
    and agent.get("model") == "gpt-5.6-luna"
    and skill.get("path") == str(skill_path)
    and server.get("command") == str(binary_path)
    and server.get("args") == ["mcp", "-config", str(config_path)]
    and server.get("cwd") == str(repo_root)
)
raise SystemExit(0 if valid else 1)
PY
}

is_unhashed_marker_owned_install() {
  [[ -f "${installed_agent}" && -f "${installed_config}" && -x "${installed_binary}" ]] || return 1
  [[ ! -L "${installed_agent}" && ! -L "${installed_config}" && ! -L "${installed_binary}" ]] || return 1
  [[ -L "${installed_skill}" ]] || return 1
  [[ "$(readlink -f "${installed_skill}")" == "$(readlink -f "${source_skill}")" ]] || return 1
  has_owner_marker "${installed_agent}" || return 1
  has_owner_marker "${installed_config}" || return 1
  has_owner_record || return 1
  ! grep -q '^binary_sha256=' "${owner_record}"
}

refuse_unowned_target() {
  local target="$1"
  local description="$2"
  if path_exists "${target}"; then
    echo "refusing to replace unrelated ${description}: ${target}" >&2
    exit 1
  fi
}

preflight_install_targets() {
  local legacy_owned="false"
  if is_legacy_owned_install || is_unhashed_marker_owned_install; then
    legacy_owned="true"
  fi

  if path_exists "${installed_skill}"; then
    if [[ ! -L "${installed_skill}" ]] \
      || [[ "$(readlink -f "${installed_skill}")" != "$(readlink -f "${source_skill}")" ]]; then
      refuse_unowned_target "${installed_skill}" "skill"
    fi
  fi

  if path_exists "${installed_agent}" \
    && ! has_owner_marker "${installed_agent}" \
    && [[ "${legacy_owned}" != "true" ]]; then
    refuse_unowned_target "${installed_agent}" "agent"
  fi
  if path_exists "${installed_config}" \
    && ! has_owner_marker "${installed_config}" \
    && [[ "${legacy_owned}" != "true" ]]; then
    refuse_unowned_target "${installed_config}" "configuration"
  fi
  if path_exists "${installed_binary}" \
    && ! has_owned_binary \
    && [[ "${legacy_owned}" != "true" ]]; then
    refuse_unowned_target "${installed_binary}" "binary"
  fi
  if path_exists "${installed_input_helper}" \
    && ! has_owned_input_helper \
    && [[ "${legacy_owned}" != "true" ]]; then
    refuse_unowned_target "${installed_input_helper}" "X11 input helper"
  fi
  if path_exists "${owner_record}" && ! has_owner_record; then
    refuse_unowned_target "${owner_record}" "ownership record"
  fi
}

write_owner_record() {
  printf '%s\n%s\n%s\n%s\n' \
    'den-services-codex-playtester-v1' \
    "repo_root=${repo_root}" \
    "binary_sha256=$(sha256sum "${installed_binary}" | awk '{print $1}')" \
    "input_helper_sha256=$(sha256sum "${installed_input_helper}" | awk '{print $1}')" \
    > "${owner_record}"
}

render_templates() {
  python3 - \
    "${agent_template}" "${installed_agent}" \
    "${config_template}" "${installed_config}" \
    "${installed_skill}/SKILL.md" "${installed_binary}" \
    "${repo_root}" "${state_dir}" "${artifact_root}" "${driver_script}" "${installed_input_helper}" \
    "${codex_config}" <<'PY'
import json
import pathlib
import sys
import tomllib

(
    agent_template,
    installed_agent,
    config_template,
    installed_config,
    skill_path,
    binary_path,
    repo_root,
    state_dir,
    artifact_root,
    driver_script,
    input_helper,
    codex_config,
) = map(pathlib.Path, sys.argv[1:])

agent = agent_template.read_text()
for marker, value in {
    "@SKILL_PATH_TOML@": skill_path,
    "@BINARY_PATH_TOML@": binary_path,
    "@CONFIG_PATH_TOML@": installed_config,
    "@REPO_ROOT_TOML@": repo_root,
}.items():
    agent = agent.replace(marker, json.dumps(str(value)))

den_reference_server = ""
if codex_config.is_file():
    try:
        root_config = tomllib.loads(codex_config.read_text())
    except tomllib.TOMLDecodeError as error:
        raise SystemExit(f"cannot discover Den reference MCP from {codex_config}: {error}")
    den_server = root_config.get("mcp_servers", {}).get("den", {})
    den_url = den_server.get("url") if isinstance(den_server, dict) else None
    if isinstance(den_url, str) and den_url.strip():
        den_reference_server = f'''[mcp_servers.den_reference]
url = {json.dumps(den_url)}
enabled = true
enabled_tools = [
  "den_knowledge_get",
  "den_knowledge_guide",
  "den_knowledge_search",
  "get_document",
]
default_tools_approval_mode = "approve"
'''
agent = agent.replace("@DEN_REFERENCE_SERVER_TOML@", den_reference_server.rstrip())
installed_agent.write_text(agent)

config = config_template.read_text()
for marker, value in {
    "@STATE_DIR_YAML@": state_dir,
    "@ARTIFACT_ROOT_YAML@": artifact_root,
    "@DRIVER_SCRIPT_YAML@": driver_script,
    "@INPUT_HELPER_YAML@": input_helper,
}.items():
    config = config.replace(marker, json.dumps(str(value)))
installed_config.write_text(config)
PY
}

install_skill_link() {
  if [[ -L "${installed_skill}" ]]; then
    [[ "$(readlink -f "${installed_skill}")" == "$(readlink -f "${source_skill}")" ]] || {
      echo "existing skill link points elsewhere: ${installed_skill}" >&2
      exit 1
    }
    return
  fi
  if [[ -e "${installed_skill}" ]]; then
    echo "refusing to replace existing non-link skill: ${installed_skill}" >&2
    exit 1
  fi
  ln -s "${source_skill}" "${installed_skill}"
}

validate_installation() {
  require_file "${installed_agent}"
  require_file "${installed_config}"
  [[ -x "${installed_binary}" ]] || { echo "binary is not executable: ${installed_binary}" >&2; exit 1; }
  [[ -x "${installed_input_helper}" ]] || { echo "input helper is not executable: ${installed_input_helper}" >&2; exit 1; }
  [[ -L "${installed_skill}" ]] || { echo "skill is not linked: ${installed_skill}" >&2; exit 1; }
  [[ "$(readlink -f "${installed_skill}")" == "$(readlink -f "${source_skill}")" ]] || {
    echo "skill link does not resolve to repository source" >&2
    exit 1
  }

  validation_dir="$(mktemp -d "${codex_root}/.playtester-check.XXXXXX")"
  trap 'rm -rf "${validation_dir}"' RETURN
  printf '%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
    | "${installed_binary}" mcp -config "${installed_config}" > "${validation_dir}/mcp.jsonl"

  python3 - \
    "${installed_agent}" "${installed_config}" "${installed_skill}/SKILL.md" \
    "${installed_binary}" "${driver_script}" "${installed_input_helper}" "${validation_dir}/mcp.jsonl" \
    "${codex_config}" <<'PY'
import json
import pathlib
import re
import sys
import tomllib

agent_path, config_path, skill_path, binary_path, driver_path, input_helper_path, mcp_path, codex_config = map(pathlib.Path, sys.argv[1:])
agent = tomllib.loads(agent_path.read_text())
expected_tools = {
    "playtest_start", "playtest_observe", "playtest_act", "playtest_inspect",
    "playtest_finish", "playtest_cancel", "playtest_get", "playtest_list",
}

assert agent["name"] == "playtester"
assert agent["model"] == "gpt-5.6-luna"
assert agent["model_reasoning_effort"] == "max"
assert agent["skills"]["config"][0]["path"] == str(skill_path)
server = agent["mcp_servers"]["den_playtest"]
assert server["command"] == str(binary_path)
assert server["args"] == ["mcp", "-config", str(config_path)]
assert set(server["enabled_tools"]) == expected_tools

root_config = tomllib.loads(codex_config.read_text()) if codex_config.is_file() else {}
root_den = root_config.get("mcp_servers", {}).get("den", {})
root_den_url = root_den.get("url") if isinstance(root_den, dict) else None
reference = agent.get("mcp_servers", {}).get("den_reference")
if isinstance(root_den_url, str) and root_den_url.strip():
    assert reference["url"] == root_den_url
    assert set(reference["enabled_tools"]) == {
        "den_knowledge_get", "den_knowledge_guide", "den_knowledge_search", "get_document",
    }
else:
    assert reference is None

config = config_path.read_text()
match = re.search(r'^\s*driver_script:\s*(.+?)\s*$', config, re.MULTILINE)
assert match, "rendered config lacks driver_script"
assert pathlib.Path(json.loads(match.group(1))) == driver_path
match = re.search(r'^\s*input_helper:\s*(.+?)\s*$', config, re.MULTILINE)
assert match, "rendered config lacks input_helper"
assert pathlib.Path(json.loads(match.group(1))) == input_helper_path

responses = [json.loads(line) for line in mcp_path.read_text().splitlines() if line.strip()]
assert responses[0]["result"]["serverInfo"]["name"] == "den-playwright-playtest"
listed = {tool["name"] for tool in responses[1]["result"]["tools"]}
assert listed == expected_tools, (listed, expected_tools)
PY

  rm -rf "${validation_dir}"
  trap - RETURN
  if grep -Fq '[mcp_servers.den_reference]' "${installed_agent}"; then
    echo "playtester installation valid: gpt-5.6-luna / max / 8 playtest tools + read-only Den references"
  else
    echo "playtester installation valid: gpt-5.6-luna / max / 8 playtest tools"
    echo "No root Den URL was discovered; pass resolved source material in the mission packet."
  fi
  echo "Start a fresh Codex task to load the installed agent, skill, and MCP server."
}

if [[ "${mode}" == "install" ]]; then
  preflight_install_targets
  mkdir -p "${codex_root}/agents" "${codex_root}/bin" "${codex_root}/skills" \
    "${codex_root}/playtester" "${state_dir}" "${artifact_root}"
  install_skill_link
  build_dir="$(mktemp -d "${codex_root}/.playtester-build.XXXXXX")"
  trap 'rm -rf "${build_dir}"' EXIT
  (
    cd "${repo_root}"
    go build -o "${build_dir}/den-playwright" ./playwright-broker/cmd/den-playwright
    go build -o "${build_dir}/den-playwright-x11-input" ./playwright-broker/cmd/playtest-x11-input
  )
  install -m 0755 "${build_dir}/den-playwright" "${installed_binary}"
  install -m 0755 "${build_dir}/den-playwright-x11-input" "${installed_input_helper}"
  render_templates
  write_owner_record
  rm -rf "${build_dir}"
  trap - EXIT
fi

validate_installation
