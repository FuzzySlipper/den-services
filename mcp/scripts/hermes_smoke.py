#!/usr/bin/env python3
"""Hermes-stability smoke harness for den-services/mcp.

The local mode is safe for CI and developer machines: it starts a disposable
fake den-core backend and a real den-services/mcp process on loopback ports.
Live den-core checks are opt-in and restore a pre-existing disposable document
after the representative write probe.
"""

from __future__ import annotations

import argparse
import contextlib
import json
import os
import signal
import socket
import subprocess
import sys
import tempfile
import textwrap
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any


EXPECTED_TOOL_COUNT = 136
MCP_PATH = "/mcp"
DEN_CORE_TOKEN_ENV = "DEN_CORE_SERVICE_TOKEN"


class SmokeError(RuntimeError):
    pass


class ReusableHTTPServer(ThreadingHTTPServer):
    allow_reuse_address = True
    daemon_threads = True


class FakeDenCore:
    def __init__(self, port: int) -> None:
        self.port = port
        self.base_url = f"http://127.0.0.1:{port}"
        self.documents: dict[tuple[str, str], dict[str, Any]] = {
            ("den-services", "mcp-smoke-disposable"): {
                "project_id": "den-services",
                "slug": "mcp-smoke-disposable",
                "title": "MCP Smoke Disposable",
                "content": "original fake smoke content",
                "doc_type": "note",
                "summary": "Disposable local smoke document.",
                "tags": ["mcp", "smoke"],
            }
        }
        self._server: ReusableHTTPServer | None = None
        self._thread: threading.Thread | None = None

    def start(self) -> None:
        fake = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, _format: str, *_args: Any) -> None:
                return

            def do_GET(self) -> None:  # noqa: N802
                if self.path != "/health":
                    self.send_error(404)
                    return
                self.send_response(200)
                self.send_header("Content-Type", "text/plain")
                self.end_headers()
                self.wfile.write(b"fake_den_core_health_ok")

            def do_POST(self) -> None:  # noqa: N802
                if self.path != MCP_PATH:
                    self.send_error(404)
                    return
                try:
                    length = int(self.headers.get("Content-Length", "0"))
                    request = json.loads(self.rfile.read(length))
                    result = fake.handle_rpc(request)
                except Exception as exc:  # pragma: no cover - diagnostic path
                    self.send_response(500)
                    self.send_header("Content-Type", "text/plain")
                    self.end_headers()
                    self.wfile.write(str(exc).encode("utf-8"))
                    return
                body = json.dumps(result).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        self._server = ReusableHTTPServer(("127.0.0.1", self.port), Handler)
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)
        self._thread.start()
        wait_for_http(f"{self.base_url}/health")

    def stop(self) -> None:
        if self._server is None:
            return
        self._server.shutdown()
        self._server.server_close()
        if self._thread is not None:
            self._thread.join(timeout=5)
        self._server = None
        self._thread = None

    def handle_rpc(self, request: dict[str, Any]) -> dict[str, Any]:
        if request.get("method") != "tools/call":
            return rpc_error(request.get("id"), -32601, "method not found")
        params = request.get("params") or {}
        name = params.get("name")
        arguments = params.get("arguments") or {}
        if name == "get_task":
            result = tool_result(
                f"fake:get_task:{arguments.get('task_id', 'missing')}",
                {"tool": "get_task", "task_id": arguments.get("task_id")},
            )
        elif name == "get_document":
            result = self.get_document(arguments)
        elif name == "store_document":
            result = self.store_document(arguments)
        else:
            result = tool_result(f"fake:{name}", {"tool": name, "arguments": arguments})
        return {"jsonrpc": "2.0", "id": request.get("id"), "result": result}

    def get_document(self, arguments: dict[str, Any]) -> dict[str, Any]:
        key = (arguments.get("project_id", ""), arguments.get("slug", ""))
        document = self.documents.get(key)
        if document is None:
            return tool_result("document not found", {"document": None}, is_error=True)
        return tool_result(json.dumps(document, sort_keys=True), {"document": document})

    def store_document(self, arguments: dict[str, Any]) -> dict[str, Any]:
        key = (arguments.get("project_id", ""), arguments.get("slug", ""))
        document = {
            "project_id": key[0],
            "slug": key[1],
            "title": arguments["title"],
            "content": arguments["content"],
            "doc_type": arguments.get("doc_type", "note"),
            "summary": arguments.get("summary"),
            "tags": arguments.get("tags"),
        }
        self.documents[key] = document
        return tool_result("fake:store_document", {"document": document})


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mode", choices=("local", "live", "both"), default=os.getenv("DEN_MCP_SMOKE_MODE", "local"))
    parser.add_argument("--den-core-url", default=os.getenv("DEN_MCP_SMOKE_DEN_CORE_URL", ""))
    parser.add_argument("--read-task-id", type=int, default=int(os.getenv("DEN_MCP_SMOKE_READ_TASK_ID", "3446")))
    parser.add_argument("--write-project", default=os.getenv("DEN_MCP_SMOKE_WRITE_PROJECT", ""))
    parser.add_argument("--write-slug", default=os.getenv("DEN_MCP_SMOKE_WRITE_SLUG", ""))
    parser.add_argument("--startup-timeout", type=float, default=float(os.getenv("DEN_MCP_SMOKE_STARTUP_TIMEOUT", "30")))
    args = parser.parse_args()

    repo_root = Path(__file__).resolve().parents[2]
    os.chdir(repo_root)

    try:
        if args.mode in ("local", "both"):
            run_local_smoke(repo_root, args.startup_timeout)
        if args.mode in ("live", "both"):
            run_live_smoke(repo_root, args)
    except SmokeError as exc:
        print(f"not ok: {exc}", file=sys.stderr)
        return 1

    print("ok: hermes stability smoke complete")
    return 0


def run_local_smoke(repo_root: Path, startup_timeout: float) -> None:
    backend_port = free_port()
    mcp_port = free_port()
    fake = FakeDenCore(backend_port)
    fake.start()
    with mcp_process(repo_root, mcp_port, fake.base_url, startup_timeout):
        mcp_url = f"http://127.0.0.1:{mcp_port}{MCP_PATH}"
        initialize(mcp_url, "local")
        healthy_tools = tools_list(mcp_url)
        assert_tool_count(healthy_tools, "local")
        print(f"ok: local tools/list returned {len(healthy_tools)} tools")

        read = tools_call(mcp_url, "get_task", {"task_id": 3446})
        assert_tool_success(read, "local read tool")
        print("ok: local read tool proxied through backend")

        search = tools_call(mcp_url, "search_documents", {"project_id": "den-services", "query": "mcp", "verbose": False})
        assert_tool_success(search, "local search_documents tool")
        print("ok: local non-representative tool proxied through backend")

        original = tools_call(
            mcp_url,
            "get_document",
            {"project_id": "den-services", "slug": "mcp-smoke-disposable", "verbose": True},
        )
        original_doc = document_from_result(original)
        smoke_doc = dict(original_doc)
        smoke_doc["content"] = f"smoke write {time.time_ns()}"
        store_document(mcp_url, smoke_doc)
        written = document_from_result(
            tools_call(
                mcp_url,
                "get_document",
                {"project_id": smoke_doc["project_id"], "slug": smoke_doc["slug"], "verbose": True},
            )
        )
        if written.get("content") != smoke_doc["content"]:
            raise SmokeError("local write did not round-trip through get_document")
        store_document(mcp_url, original_doc)
        print("ok: local write tool proxied through backend and restored disposable state")

        fake.stop()
        wait_for_closed(fake.base_url)
        wait_for_http(f"http://127.0.0.1:{mcp_port}/health")
        print("ok: mcp /health stayed healthy during backend outage")

        unavailable_tools = tools_list(mcp_url)
        if normalize_json(healthy_tools) != normalize_json(unavailable_tools):
            raise SmokeError("tools/list changed while backend was unavailable")
        print("ok: tools/list remained identical while backend was unavailable")

        outage = tools_call(mcp_url, "get_task", {"task_id": 3446})
        structured = assert_tool_failure(outage, "den_backend_unavailable")
        if not structured.get("retryable"):
            raise SmokeError("backend outage failure was not retryable")
        if structured.get("circuit_state") != "unavailable":
            raise SmokeError("backend outage did not include circuit_state=unavailable")
        print("ok: backend outage returned retryable den_backend_unavailable")

        fake.start()
        recovered = tools_call(mcp_url, "get_task", {"task_id": 3446})
        assert_tool_success(recovered, "local recovered read tool")
        recovered_tools = tools_list(mcp_url)
        if normalize_json(healthy_tools) != normalize_json(recovered_tools):
            raise SmokeError("tools/list changed after backend recovery")
        print("ok: backend recovered in the same MCP process")
    fake.stop()


def run_live_smoke(repo_root: Path, args: argparse.Namespace) -> None:
    if not args.den_core_url:
        raise SmokeError("--den-core-url or DEN_MCP_SMOKE_DEN_CORE_URL is required for live mode")
    mcp_port = free_port()
    with mcp_process(repo_root, mcp_port, args.den_core_url.rstrip("/"), args.startup_timeout):
        mcp_url = f"http://127.0.0.1:{mcp_port}{MCP_PATH}"
        initialize(mcp_url, "live")
        live_tools = tools_list(mcp_url)
        assert_tool_count(live_tools, "live")
        print(f"ok: live tools/list returned {len(live_tools)} tools")

        read = tools_call(mcp_url, "get_task", {"task_id": args.read_task_id, "verbose": False})
        assert_tool_success(read, "live read tool")
        print("ok: live read tool proxied to den-core")

        search = tools_call(mcp_url, "search_documents", {"project_id": "den-services", "query": "mcp", "verbose": False})
        assert_tool_success(search, "live search_documents tool")
        print("ok: live non-representative tool proxied to den-core")

        if args.write_project and args.write_slug:
            run_live_write_restore(mcp_url, args.write_project, args.write_slug)
        else:
            print("ok: live write skipped; set DEN_MCP_SMOKE_WRITE_PROJECT and DEN_MCP_SMOKE_WRITE_SLUG to enable restore-only write smoke")


def run_live_write_restore(mcp_url: str, project_id: str, slug: str) -> None:
    original = tools_call(mcp_url, "get_document", {"project_id": project_id, "slug": slug, "verbose": True})
    original_doc = document_from_result(original)
    if original_doc.get("project_id") != project_id or original_doc.get("slug") != slug:
        raise SmokeError("live disposable document lookup returned an unexpected document")
    smoke_doc = dict(original_doc)
    smoke_doc["content"] = f"{original_doc.get('content', '')}\n\nMCP smoke write {time.time_ns()}"
    try:
        store_document(mcp_url, smoke_doc)
        written = document_from_result(
            tools_call(mcp_url, "get_document", {"project_id": project_id, "slug": slug, "verbose": True})
        )
        if written.get("content") != smoke_doc["content"]:
            raise SmokeError("live write did not round-trip through get_document")
    finally:
        store_document(mcp_url, original_doc)
    print("ok: live write tool proxied to den-core and restored disposable document")


@contextlib.contextmanager
def mcp_process(repo_root: Path, mcp_port: int, backend_url: str, startup_timeout: float):
    with tempfile.TemporaryDirectory(prefix="den-services-mcp-smoke-") as temp_dir:
        temp_path = Path(temp_dir)
        routes_path = temp_path / "routes.yaml"
        config_path = temp_path / "config.yaml"
        routes_path.write_text((repo_root / "mcp" / "routes.example.yaml").read_text(encoding="utf-8"), encoding="utf-8")
        config_path.write_text(
            textwrap.dedent(
                f"""
                server:
                  listen_addr: "127.0.0.1:{mcp_port}"
                  mcp_endpoint_path: "{MCP_PATH}"
                  read_header_timeout: "5s"

                security:
                  service_token_env: "DEN_MCP_SERVICE_TOKEN"
                  allow_unauthenticated_local_dev: true

                routes:
                  table_path: "{routes_path}"

                backends:
                  - name: "den-core"
                    base_url: "{backend_url}"
                    health_path: "/health"
                    timeout: "1s"
                    service_token_env: "{DEN_CORE_TOKEN_ENV}"
                """
            ).lstrip(),
            encoding="utf-8",
        )
        log_path = temp_path / "mcp.log"
        log_file = log_path.open("w", encoding="utf-8")
        env = os.environ.copy()
        env["MCP_CONFIG_PATH"] = str(config_path)
        proc = subprocess.Popen(
            ["go", "run", "./mcp/cmd/mcp"],
            cwd=repo_root,
            env=env,
            stdout=log_file,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
        try:
            wait_for_http(f"http://127.0.0.1:{mcp_port}/health", timeout=startup_timeout)
            yield
        finally:
            terminate_process_group(proc)
            log_file.close()
            if proc.returncode not in (0, -signal.SIGTERM, -signal.SIGKILL, None):
                with contextlib.suppress(OSError):
                    print(log_path.read_text(encoding="utf-8"), file=sys.stderr)


def initialize(mcp_url: str, label: str) -> None:
    response = rpc(mcp_url, "initialize", {"protocolVersion": "2025-11-25"})
    result = response.get("result") or {}
    if result.get("serverInfo", {}).get("name") != "den-services-mcp":
        raise SmokeError(f"{label} initialize returned unexpected serverInfo")
    print(f"ok: {label} initialize")


def tools_list(mcp_url: str) -> list[dict[str, Any]]:
    response = rpc(mcp_url, "tools/list", {})
    tools = (response.get("result") or {}).get("tools")
    if not isinstance(tools, list):
        raise SmokeError("tools/list did not return a tools array")
    return tools


def tools_call(mcp_url: str, name: str, arguments: dict[str, Any]) -> dict[str, Any]:
    response = rpc(mcp_url, "tools/call", {"name": name, "arguments": arguments})
    result = response.get("result")
    if not isinstance(result, dict):
        raise SmokeError(f"tools/call {name} did not return an object result")
    return result


def rpc(mcp_url: str, method: str, params: dict[str, Any]) -> dict[str, Any]:
    response = post_json(mcp_url, {"jsonrpc": "2.0", "id": int(time.time_ns()), "method": method, "params": params})
    if response.get("error"):
        raise SmokeError(f"{method} returned JSON-RPC error: {response['error']}")
    return response


def store_document(mcp_url: str, document: dict[str, Any]) -> dict[str, Any]:
    payload = {
        "project_id": document["project_id"],
        "slug": document["slug"],
        "title": document.get("title") or document["slug"],
        "content": document.get("content") or "",
        "doc_type": document.get("doc_type") or "note",
        "summary": document.get("summary"),
        "tags": document.get("tags"),
        "verbose": True,
    }
    result = tools_call(mcp_url, "store_document", payload)
    assert_tool_success(result, "store_document")
    return result


def document_from_result(result: dict[str, Any]) -> dict[str, Any]:
    assert_tool_success(result, "get_document")
    structured = result.get("structuredContent")
    if isinstance(structured, dict):
        document = structured.get("document") or structured.get("result") or structured
        if isinstance(document, dict):
            return document
    for item in result.get("content") or []:
        if item.get("type") != "text":
            continue
        text = item.get("text", "")
        with contextlib.suppress(json.JSONDecodeError):
            parsed = json.loads(text)
            if isinstance(parsed, dict):
                return parsed.get("document") if isinstance(parsed.get("document"), dict) else parsed
    raise SmokeError("could not extract document payload from get_document result")


def assert_tool_count(tools: list[dict[str, Any]], label: str) -> None:
    if len(tools) != EXPECTED_TOOL_COUNT:
        raise SmokeError(f"{label} tools/list returned {len(tools)} tools, want {EXPECTED_TOOL_COUNT}")


def assert_tool_success(result: dict[str, Any], label: str) -> None:
    if result.get("isError"):
        raise SmokeError(f"{label} returned error result: {result}")


def assert_tool_failure(result: dict[str, Any], expected_error: str) -> dict[str, Any]:
    if not result.get("isError"):
        raise SmokeError(f"expected {expected_error} error result, got success")
    structured = result.get("structuredContent")
    if not isinstance(structured, dict):
        raise SmokeError("error result did not include structuredContent")
    if structured.get("error") != expected_error:
        raise SmokeError(f"error = {structured.get('error')}, want {expected_error}")
    return structured


def post_json(url: str, payload: dict[str, Any], timeout: float = 5) -> dict[str, Any]:
    data = json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"}, method="POST")
    with urllib.request.urlopen(request, timeout=timeout) as response:
        return json.loads(response.read().decode("utf-8"))


def wait_for_http(url: str, timeout: float = 10) -> None:
    deadline = time.monotonic() + timeout
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=1) as response:
                if 200 <= response.status < 300:
                    return
        except Exception as exc:  # pragma: no cover - depends on timing
            last_error = exc
        time.sleep(0.1)
    raise SmokeError(f"timed out waiting for {url}: {last_error}")


def wait_for_closed(base_url: str, timeout: float = 10) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            urllib.request.urlopen(f"{base_url}/health", timeout=0.5)
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            return
        time.sleep(0.1)
    raise SmokeError(f"backend did not stop accepting health checks: {base_url}")


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def normalize_json(value: Any) -> str:
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def rpc_error(request_id: Any, code: int, message: str) -> dict[str, Any]:
    return {"jsonrpc": "2.0", "id": request_id, "error": {"code": code, "message": message}}


def tool_result(text: str, structured: dict[str, Any], is_error: bool = False) -> dict[str, Any]:
    return {
        "content": [{"type": "text", "text": text}],
        "isError": is_error,
        "structuredContent": structured,
    }


def terminate_process_group(proc: subprocess.Popen[Any]) -> None:
    if proc.poll() is None:
        with contextlib.suppress(ProcessLookupError):
            os.killpg(proc.pid, signal.SIGTERM)
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            with contextlib.suppress(ProcessLookupError):
                os.killpg(proc.pid, signal.SIGKILL)
            proc.wait(timeout=5)


if __name__ == "__main__":
    raise SystemExit(main())
