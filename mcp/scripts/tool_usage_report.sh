#!/usr/bin/env bash
set -euo pipefail

unit="den-go@mcp.service"
since="24 hours ago"
until="now"
journalctl_bin="${JOURNALCTL_BIN:-journalctl}"
jq_bin="${JQ_BIN:-jq}"

usage() {
  echo "usage: $0 [--since TIME] [--until TIME] [--unit SYSTEMD_UNIT]" >&2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --since)
      since="${2:?--since requires a value}"
      shift 2
      ;;
    --until)
      until="${2:?--until requires a value}"
      shift 2
      ;;
    --unit)
      unit="${2:?--unit requires a value}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

{
  echo $'count\ttool\tbackend\toutcome\tretryable'
  "${journalctl_bin}" --unit "${unit}" --since "${since}" --until "${until}" --output cat --no-pager |
    "${jq_bin}" -Rr 'fromjson? | select(.msg == "mcp_tool_call") | [.tool, .backend, .outcome, (.retryable | tostring)] | @tsv' |
    awk -F '\t' '
      { counts[$1 FS $2 FS $3 FS $4]++ }
      END {
        for (key in counts) {
          print counts[key] "\t" key
        }
      }
    ' |
    sort -t $'\t' -k1,1nr -k2,2
}
