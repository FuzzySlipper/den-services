#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s [--check]\n' "${0##*/}" >&2
}

check_only=false
case "${1:-}" in
  "") ;;
  --check)
    check_only=true
    if [[ $# -ne 1 ]]; then
      usage
      exit 2
    fi
    ;;
  *)
    usage
    exit 2
    ;;
esac

if [[ -z "${HOME:-}" ]]; then
  printf 'install-den-tool: HOME must be set\n' >&2
  exit 1
fi
if ! command -v go >/dev/null 2>&1; then
  printf 'install-den-tool: go is required on PATH\n' >&2
  exit 1
fi
if ! command -v install >/dev/null 2>&1; then
  printf 'install-den-tool: install is required on PATH\n' >&2
  exit 1
fi

script_dir=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
bin_dir="$HOME/.local/bin"
destination="$bin_dir/den-tool"
owner_marker="$destination.owner"

has_owned_marker() {
  [[ -f "$owner_marker" ]] && grep -Fqx 'den-tool ownership: den-tool' "$owner_marker"
}

has_owned_version_probe() {
  [[ -x "$destination" ]] || return 1
  local version_output
  version_output=$("$destination" --version 2>/dev/null || true)
  [[ "$version_output" == den-tool\ version\ * ]]
}

is_owned_install() {
  has_owned_marker && has_owned_version_probe
}

if [[ "$check_only" == true ]]; then
  if [[ ! -f "$destination" || ! -x "$destination" || -L "$destination" ]]; then
    printf 'install-den-tool: no owned executable at %s\n' "$destination" >&2
    exit 1
  fi
  if ! is_owned_install; then
    printf 'install-den-tool: refusing unrelated binary at %s\n' "$destination" >&2
    exit 1
  fi
  source_version=$(cd "$repo_root" && go run ./cmd/den-tool --version)
  installed_version=$("$destination" --version)
  if [[ "$source_version" != "$installed_version" ]]; then
    printf 'install-den-tool: installed version drifted (source: %s; installed: %s)\n' "$source_version" "$installed_version" >&2
    exit 1
  fi
  printf 'den-tool installation is healthy: %s\n' "$destination"
  exit 0
fi

if [[ -e "$destination" || -L "$destination" ]]; then
  if [[ -L "$destination" ]] || ! is_owned_install; then
    printf 'install-den-tool: refusing to overwrite unrelated binary at %s\n' "$destination" >&2
    exit 1
  fi
fi

mkdir -p "$bin_dir"
temporary_binary=$(mktemp "$bin_dir/.den-tool.XXXXXX")
temporary_marker=$(mktemp "$bin_dir/.den-tool-owner.XXXXXX")
cleanup() {
  rm -f -- "$temporary_binary" "$temporary_marker"
}
trap cleanup EXIT

(cd "$repo_root" && go build -o "$temporary_binary" ./cmd/den-tool)
source_version=$("$temporary_binary" --version)
install -m0755 "$temporary_binary" "$destination"
printf 'den-tool ownership: den-tool\n%s\n' "$source_version" >"$temporary_marker"
install -m0644 "$temporary_marker" "$owner_marker"

printf 'installed %s\n' "$destination"
printf '%s\n' "$source_version"
