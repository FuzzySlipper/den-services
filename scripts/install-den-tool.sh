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
if ! command -v sha256sum >/dev/null 2>&1; then
  printf 'install-den-tool: sha256sum is required on PATH\n' >&2
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

recorded_value() {
  sed -n "s/^$1: //p" "$owner_marker"
}

source_digest() {
  (
    cd "$repo_root"
    find cmd/den-tool -maxdepth 1 -type f \( -name '*.go' -o -name '*.json' \) -print0 \
      | sort -z \
      | xargs -0 sha256sum
    sha256sum go.mod go.sum
  ) | sha256sum | awk '{print $1}'
}

has_owned_binary_hash() {
  has_owned_marker || return 1
  local expected_hash actual_hash
  expected_hash=$(recorded_value binary-sha256)
  [[ -n "$expected_hash" ]] || return 1
  actual_hash=$(sha256sum "$destination" | awk '{print $1}')
  [[ "$actual_hash" == "$expected_hash" ]]
}

has_owned_version_probe() {
  [[ -x "$destination" ]] || return 1
  local version_output
  version_output=$("$destination" --version 2>/dev/null || true)
  [[ "$version_output" == den-tool\ version\ * ]]
}

is_owned_install() {
  has_owned_binary_hash && has_owned_version_probe
}

is_legacy_owned_install() {
  has_owned_marker && [[ -z "$(recorded_value binary-sha256)" ]] && has_owned_version_probe
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
  expected_source_digest=$(recorded_value source-sha256)
  current_source_digest=$(source_digest)
  if [[ -z "$expected_source_digest" || "$current_source_digest" != "$expected_source_digest" ]]; then
    printf 'install-den-tool: installed source identity drifted; reinstall required\n' >&2
    exit 1
  fi
  printf 'den-tool installation is healthy: %s\n' "$destination"
  exit 0
fi

if [[ -e "$destination" || -L "$destination" ]]; then
  if [[ -L "$destination" ]] || { ! is_owned_install && ! is_legacy_owned_install; }; then
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
current_source_digest=$(source_digest)
install -m0755 "$temporary_binary" "$destination"
installed_binary_digest=$(sha256sum "$destination" | awk '{print $1}')
printf 'den-tool ownership: den-tool\n%s\nsource-sha256: %s\nbinary-sha256: %s\n' \
  "$source_version" "$current_source_digest" "$installed_binary_digest" >"$temporary_marker"
install -m0644 "$temporary_marker" "$owner_marker"

printf 'installed %s\n' "$destination"
printf '%s\n' "$source_version"
