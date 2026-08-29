#!/usr/bin/env python3
"""Minimal local Vault endpoint for the Sub2API Heartbeat worker.

The process serves only session-bound credentials from a local JSON file. It is
intended to run behind Caddy on a private Docker network; it does not provide a
dashboard or an administrative write API.
"""

from __future__ import annotations

import argparse
import ipaddress
import json
import os
import re
import threading
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs, urlsplit

SESSION_KEY_RE = re.compile(r"^[0-9a-f]{32}$")
MAX_VAULT_BYTES = 1 << 20
MAX_KEYS = 100
DEFAULT_ALLOWED_IP = "154.37.212.18"
ALLOWED_CREDENTIAL_FIELDS = {
    "base_url",
    "api_protocol",
    "account_mode",
    "tokenrhythm_cookie",
    "tr_session",
    "tr_csrf",
    "user_agent",
    "header_overrides",
}


def _ip_in_allowlist(value: str, networks: tuple[ipaddress._BaseNetwork, ...]) -> bool:
    try:
        address = ipaddress.ip_address(value.strip())
    except ValueError:
        return False
    return any(address in network for network in networks)


def _parse_networks(raw: str) -> tuple[ipaddress._BaseNetwork, ...]:
    networks = []
    for item in raw.split(","):
        item = item.strip()
        if not item:
            continue
        try:
            networks.append(ipaddress.ip_network(item, strict=False))
        except ValueError as exc:
            raise ValueError(f"invalid network: {item}") from exc
    if not networks:
        raise ValueError("at least one allowed network is required")
    return tuple(networks)


def _read_json(path: Path) -> dict[str, Any]:
    if path.stat().st_size > MAX_VAULT_BYTES:
        raise ValueError("vault file exceeds size limit")
    with path.open("rb") as handle:
        raw = handle.read(MAX_VAULT_BYTES + 1)
    if len(raw) > MAX_VAULT_BYTES:
        raise ValueError("vault file exceeds size limit")
    value = json.loads(raw.decode("utf-8"))
    if not isinstance(value, dict):
        raise ValueError("vault root must be an object")
    return value


def _session_keys(document: dict[str, Any], session_key: str) -> list[dict[str, Any]]:
    # New writers may use {"sessions": {session: {"keys": [...]}}}; the
    # original key-checker contract used a top-level session-key map.
    nested = document.get("sessions")
    # Prefer the wrapped form when it contains this session, but fall back to
    # the original top-level map so the publisher can migrate incrementally.
    sessions = nested if isinstance(nested, dict) and session_key in nested else document
    if not isinstance(sessions, dict):
        return []
    entry = sessions.get(session_key)
    if isinstance(entry, list):
        candidates = entry
    elif isinstance(entry, dict):
        candidates = entry.get("keys", [])
    else:
        return []
    if not isinstance(candidates, list):
        return []
    return [item for item in candidates if isinstance(item, dict)][:MAX_KEYS]


def _public_key(item: dict[str, Any]) -> dict[str, Any] | None:
    key = item.get("key")
    provider = item.get("provider")
    if not isinstance(key, str) or not key.strip() or not isinstance(provider, str):
        return None
    output: dict[str, Any] = {"key": key.strip(), "provider": provider.strip().lower()}
    credentials = item.get("credentials")
    if isinstance(credentials, dict):
        filtered = {
            name: value
            for name, value in credentials.items()
            if name in ALLOWED_CREDENTIAL_FIELDS
        }
        if filtered:
            output["credentials"] = filtered
    for name in ("base_url", "api_protocol", "account_mode", "tokenrhythm_cookie", "tr_session", "tr_csrf", "user_agent", "header_overrides"):
        if name in item and name not in output:
            output[name] = item[name]
    return output


class VaultHandler(BaseHTTPRequestHandler):
    server: "VaultHTTPServer"

    def log_message(self, format: str, *args: object) -> None:
        # Never use BaseHTTPRequestHandler's request-line log: it includes the
        # query string and would therefore persist the session credential.
        self.server.log(format.split("%", 1)[0].strip())

    def _respond(self, status: HTTPStatus, payload: dict[str, Any]) -> None:
        body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("Pragma", "no-cache")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.end_headers()
        self.wfile.write(body)

    def _source_ip_allowed(self) -> bool:
        peer = self.client_address[0]
        if not _ip_in_allowlist(peer, self.server.trusted_proxy_networks):
            return False
        forwarded = self.headers.get("X-Real-IP", "").strip()
        if not forwarded:
            forwarded = self.headers.get("X-Forwarded-For", "").split(",", 1)[0].strip()
        return bool(forwarded) and _ip_in_allowlist(forwarded, self.server.allowed_networks)

    def do_GET(self) -> None:  # noqa: N802 - stdlib handler API
        parsed = urlsplit(self.path)
        if parsed.path == "/healthz":
            # Health checks run locally and do not expose vault state.
            if _ip_in_allowlist(self.client_address[0], self.server.trusted_proxy_networks):
                self._respond(HTTPStatus.OK, {"ok": True})
            else:
                self._respond(HTTPStatus.FORBIDDEN, {"ok": False, "error": "forbidden"})
            return
        if parsed.path != "/api/hb/keys":
            self._respond(HTTPStatus.NOT_FOUND, {"ok": False, "error": "not_found"})
            return
        if not self._source_ip_allowed():
            self._respond(HTTPStatus.FORBIDDEN, {"ok": False, "error": "forbidden"})
            return
        values = parse_qs(parsed.query, keep_blank_values=True).get("session_key", [])
        if len(values) != 1 or not SESSION_KEY_RE.fullmatch(values[0]):
            self._respond(HTTPStatus.BAD_REQUEST, {"ok": False, "error": "invalid_session_key"})
            return
        session_key = values[0]
        try:
            keys = [_public_key(item) for item in _session_keys(_read_json(self.server.vault_file), session_key)]
        except (OSError, UnicodeError, ValueError, json.JSONDecodeError):
            self._respond(HTTPStatus.SERVICE_UNAVAILABLE, {"ok": False, "error": "vault_unavailable"})
            return
        public_keys = [item for item in keys if item is not None]
        if not public_keys:
            self._respond(HTTPStatus.NOT_FOUND, {"ok": False, "error": "session_empty"})
            return
        self._respond(HTTPStatus.OK, {"ok": True, "keys": public_keys})


class VaultHTTPServer(ThreadingHTTPServer):
    daemon_threads = True
    allow_reuse_address = True

    def __init__(self, address: tuple[str, int], vault_file: Path, allowed_networks: tuple[ipaddress._BaseNetwork, ...], trusted_proxy_networks: tuple[ipaddress._BaseNetwork, ...]) -> None:
        super().__init__(address, VaultHandler)
        self.vault_file = vault_file
        self.allowed_networks = allowed_networks
        self.trusted_proxy_networks = trusted_proxy_networks
        self._log_lock = threading.Lock()

    def log(self, message: str) -> None:
        # Keep operational logs free of request paths, query values and bodies.
        with self._log_lock:
            print(f"vault event={message}", flush=True)


def main() -> int:
    parser = argparse.ArgumentParser(description="Serve the local Sub2API Heartbeat Vault")
    parser.add_argument("--bind", default=os.environ.get("VAULT_BIND", "127.0.0.1"))
    parser.add_argument("--port", type=int, default=int(os.environ.get("VAULT_PORT", "44444")))
    parser.add_argument("--vault-file", type=Path, default=Path(os.environ.get("VAULT_FILE", "/opt/key-checker/.hb_vault.json")))
    parser.add_argument("--allowed-ip", default=os.environ.get("VAULT_ALLOWED_IPS", DEFAULT_ALLOWED_IP), help="comma-separated Sub2API worker IP/CIDR list")
    parser.add_argument("--trusted-proxy", default=os.environ.get("VAULT_TRUSTED_PROXIES", "127.0.0.1/32,::1/128"), help="comma-separated local proxy IP/CIDR list")
    args = parser.parse_args()
    if not 1 <= args.port <= 65535:
        raise SystemExit("port is outside the valid range")
    if args.bind in {"0.0.0.0", "::"} and os.environ.get("VAULT_ALLOW_WIDE_BIND") != "1":
        # A sidecar can opt in explicitly; host deployments should bind to a
        # private interface instead of exposing the credential endpoint.
        raise SystemExit("wide bind requires VAULT_ALLOW_WIDE_BIND=1")
    allowed = _parse_networks(args.allowed_ip)
    trusted = _parse_networks(args.trusted_proxy)
    server = VaultHTTPServer((args.bind, args.port), args.vault_file, allowed, trusted)
    server.log(f"started bind={args.bind} port={args.port}")
    try:
        server.serve_forever(poll_interval=0.5)
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
