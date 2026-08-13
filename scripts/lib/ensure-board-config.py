#!/usr/bin/env python3
"""Idempotently add Board ownership routes/backends to preserved service YAML."""

from __future__ import annotations

import pathlib
import re
import sys


BOARD_BACKEND = [
    '- name: "board"\n',
    '  base_url: "http://127.0.0.1:8100"\n',
    '  health_path: "/health"\n',
    '  timeout: "3s"\n',
    '  service_token_env: "DEN_BOARD_SERVICE_TOKEN"\n',
]

BOARD_ROUTES = """
  - name: "board-project-routes"
    path_pattern: "/v1/projects/{project_id}/board"
    methods: ["GET", "POST"]
    legacy_upstream_url: "http://127.0.0.1:8100"
    successor_upstream_url: "http://127.0.0.1:8100"
    successor_mode: "always"
    caller_auth:
      bearer_token: "${DEN_GATEWAY_WEB_TOKEN}"
    successor_auth:
      bearer_token: "${DEN_GATEWAY_BOARD_UPSTREAM_TOKEN}"

  - name: "board-item-routes"
    path_pattern: "/v1/board"
    methods: ["GET", "POST", "DELETE"]
    legacy_upstream_url: "http://127.0.0.1:8100"
    successor_upstream_url: "http://127.0.0.1:8100"
    successor_mode: "always"
    caller_auth:
      bearer_token: "${DEN_GATEWAY_WEB_TOKEN}"
    successor_auth:
      bearer_token: "${DEN_GATEWAY_BOARD_UPSTREAM_TOKEN}"
""".lstrip("\n")


def ensure_mcp_backend(path: pathlib.Path) -> None:
    text = path.read_text(encoding="utf-8")
    if re.search(r"(?m)^\s*-\s+name:\s*['\"]?board['\"]?\s*$", text):
        return
    lines = text.splitlines(keepends=True)
    start = next(
        (i for i, line in enumerate(lines) if re.match(r"^backends:\s*(?:#.*)?$", line.rstrip("\n"))),
        None,
    )
    if start is None:
        raise SystemExit(f"missing top-level backends list in {path}")
    item_indent: str | None = None
    end = len(lines)
    for index in range(start + 1, len(lines)):
        line = lines[index]
        stripped = line.lstrip(" \t")
        if not stripped.strip() or stripped.startswith("#"):
            continue
        indent = line[: len(line) - len(stripped)]
        if item_indent is None and stripped.startswith("- "):
            item_indent = indent
            continue
        if item_indent is not None and not indent and not stripped.startswith("- "):
            end = index
            break
    if item_indent is None:
        item_indent = "  "
    block = [item_indent + line if index == 0 else item_indent + line for index, line in enumerate(BOARD_BACKEND)]
    lines[end:end] = block
    path.write_text("".join(lines), encoding="utf-8")


def ensure_gateway_routes(path: pathlib.Path) -> None:
    text = path.read_text(encoding="utf-8")
    if "board-project-routes" in text and "board-item-routes" in text:
        return
    lines = text.splitlines(keepends=True)
    insertion = len(lines)
    for index, line in enumerate(lines):
        if re.match(r'^\s*path_pattern:\s*["\']?/v1/projects["\']?\s*$', line.rstrip("\n")):
            for candidate in range(index - 1, -1, -1):
                if re.match(r"^\s*-\s+name:", lines[candidate]):
                    insertion = candidate
                    break
            break
    missing = BOARD_ROUTES
    if "board-project-routes" in text:
        missing = missing[missing.index('  - name: "board-item-routes"') :]
    elif "board-item-routes" in text:
        missing = missing[: missing.index('  - name: "board-item-routes"')]
    if insertion and lines[insertion - 1].strip():
        missing = "\n" + missing
    lines[insertion:insertion] = [missing]
    path.write_text("".join(lines), encoding="utf-8")


def main() -> None:
    if len(sys.argv) != 3 or sys.argv[1] not in {"gateway-routes", "mcp-backend"}:
        raise SystemExit("usage: ensure-board-config.py gateway-routes|mcp-backend PATH")
    path = pathlib.Path(sys.argv[2])
    if sys.argv[1] == "gateway-routes":
        ensure_gateway_routes(path)
    else:
        ensure_mcp_backend(path)


if __name__ == "__main__":
    main()
