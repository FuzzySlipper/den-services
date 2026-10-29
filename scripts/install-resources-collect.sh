#!/usr/bin/env bash
# Install the Resources collector on den-agents (config and the always-on
# systemd service) and the asset-resources wrapper. den-tool itself is
# rebuilt by scripts/deploy.sh. Safe to rerun; it only reads the hosts.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
config_dir="$HOME/.config/den-resources-collect"

den_tool="$HOME/.local/bin/den-tool"
if ! "$den_tool" resources --help 2>&1 | grep -q 'collect'; then
  printf 'install-resources-collect: %s has no "resources collect"; deploy den-services first\n' "$den_tool" >&2
  exit 1
fi
install -d -m 0750 "$config_dir"
install -m 0640 "$repo_root/deployment/resources-collect/den-agents.json" "$config_dir/config.json"
install -m 0755 "$repo_root/deployment/resources-collect/asset-resources" "$HOME/.local/bin/asset-resources"
sudo install -m 0644 "$repo_root/deployment/resources-collect/den-resources-collect.service" /etc/systemd/system/den-resources-collect.service
sudo systemctl daemon-reload
sudo systemctl enable den-resources-collect.service
sudo systemctl restart den-resources-collect.service
printf 'install-resources-collect: running; check with: journalctl -u den-resources-collect.service -n 20\n'
