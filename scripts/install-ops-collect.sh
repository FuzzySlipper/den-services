#!/usr/bin/env bash
# Install the Ops evidence collector on den-agents: its config and the
# 5-minute systemd timer. den-tool itself is rebuilt by scripts/deploy.sh.
# Safe to rerun; it never touches backup jobs.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
config_dir="$HOME/.config/den-ops-collect"

den_tool="$HOME/.local/bin/den-tool"
if ! "$den_tool" ops --help 2>&1 | grep -q 'ops collect'; then
  printf 'install-ops-collect: %s has no "ops collect"; deploy den-services first\n' "$den_tool" >&2
  exit 1
fi
install -d -m 0750 "$config_dir"
install -m 0640 "$repo_root/deployment/ops-collect/den-agents.json" "$config_dir/config.json"
sudo install -m 0644 "$repo_root/deployment/ops-collect/den-ops-collect.service" /etc/systemd/system/den-ops-collect.service
sudo install -m 0644 "$repo_root/deployment/ops-collect/den-ops-collect.timer" /etc/systemd/system/den-ops-collect.timer
sudo systemctl daemon-reload
sudo systemctl enable --now den-ops-collect.timer
printf 'install-ops-collect: timer enabled; check with: systemctl status den-ops-collect.service\n'
