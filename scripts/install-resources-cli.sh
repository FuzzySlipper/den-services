#!/usr/bin/env bash
# Copy den-tool and the asset-resources wrapper to other Linux hosts so
# "asset-resources run" keeps reporting from them, e.g.:
#   scripts/install-resources-cli.sh den-nimo den-m5 "-F $HOME/server-access/gpu-lab/config target"
# Each argument is the ssh arguments for one host, split on spaces.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
den_tool="$HOME/.local/bin/den-tool"
[[ $# -gt 0 ]] || { sed -n 2,5p "$0" >&2; exit 2; }
for target in "$@"; do
  read -r -a ssh_args <<<"$target"
  ssh "${ssh_args[@]}" 'mkdir -p ~/.local/bin'
  ssh "${ssh_args[@]}" 'cat > ~/.local/bin/.den-tool.new && chmod 0755 ~/.local/bin/.den-tool.new && mv ~/.local/bin/.den-tool.new ~/.local/bin/den-tool' <"$den_tool"
  ssh "${ssh_args[@]}" 'cat > ~/.local/bin/asset-resources && chmod 0755 ~/.local/bin/asset-resources' \
    <"$repo_root/deployment/resources-collect/asset-resources"
  ssh "${ssh_args[@]}" '~/.local/bin/asset-resources --help >/dev/null 2>&1; ~/.local/bin/den-tool --version'
done
