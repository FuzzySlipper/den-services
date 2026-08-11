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
installed_config="${codex_root}/playtester/config.yaml"
state_dir="${codex_root}/playtester/state"
artifact_root="${codex_root}/playtester/runs"
driver_script="${repo_root}/playwright-broker/driver/playtest-driver.mjs"

require_file() {
  [[ -f "$1" ]] || { echo "missing required file: $1" >&2; exit 1; }
}

require_file "${source_skill}/SKILL.md"
require_file "${agent_template}"
require_file "${config_template}"
require_file "${driver_script}"

render_templates() {
  python3 - \
    "${agent_template}" "${installed_agent}" \
    "${config_template}" "${installed_config}" \
    "${installed_skill}/SKILL.md" "${installed_binary}" \
    "${repo_root}" "${state_dir}" "${artifact_root}" "${driver_script}" <<'PY'
import json
import pathlib
import sys

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
) = map(pathlib.Path, sys.argv[1:])

agent = agent_template.read_text()
for marker, value in {
    "@SKILL_PATH_TOML@": skill_path,
    "@BINARY_PATH_TOML@": binary_path,
    "@CONFIG_PATH_TOML@": installed_config,
    "@REPO_ROOT_TOML@": repo_root,
}.items():
    agent = agent.replace(marker, json.dumps(str(value)))
installed_agent.write_text(agent)

config = config_template.read_text()
for marker, value in {
    "@STATE_DIR_YAML@": state_dir,
    "@ARTIFACT_ROOT_YAML@": artifact_root,
    "@DRIVER_SCRIPT_YAML@": driver_script,
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
    "${installed_binary}" "${driver_script}" "${validation_dir}/mcp.jsonl" <<'PY'
import json
import pathlib
import re
import sys
import tomllib

agent_path, config_path, skill_path, binary_path, driver_path, mcp_path = map(pathlib.Path, sys.argv[1:])
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

config = config_path.read_text()
match = re.search(r'^\s*driver_script:\s*(.+?)\s*$', config, re.MULTILINE)
assert match, "rendered config lacks driver_script"
assert pathlib.Path(json.loads(match.group(1))) == driver_path

responses = [json.loads(line) for line in mcp_path.read_text().splitlines() if line.strip()]
assert responses[0]["result"]["serverInfo"]["name"] == "den-playwright-playtest"
listed = {tool["name"] for tool in responses[1]["result"]["tools"]}
assert listed == expected_tools, (listed, expected_tools)
PY

  rm -rf "${validation_dir}"
  trap - RETURN
  echo "playtester installation valid: gpt-5.6-luna / max / 8 playtest tools"
  echo "Start a fresh Codex task to load the installed agent, skill, and MCP server."
}

if [[ "${mode}" == "install" ]]; then
  mkdir -p "${codex_root}/agents" "${codex_root}/bin" "${codex_root}/skills" \
    "${codex_root}/playtester" "${state_dir}" "${artifact_root}"
  install_skill_link
  build_dir="$(mktemp -d "${codex_root}/.playtester-build.XXXXXX")"
  trap 'rm -rf "${build_dir}"' EXIT
  (
    cd "${repo_root}"
    go build -o "${build_dir}/den-playwright" ./playwright-broker/cmd/den-playwright
  )
  install -m 0755 "${build_dir}/den-playwright" "${installed_binary}"
  render_templates
  rm -rf "${build_dir}"
  trap - EXIT
fi

validate_installation
