#!/usr/bin/env python3
import base64
import hashlib
import json
import os
import socket
import time

import requests
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

base_url = os.environ["INGEST_URL"].rstrip("/")
headers = {
    "CF-Access-Client-Id": os.environ["CF_ACCESS_CLIENT_ID"],
    "CF-Access-Client-Secret": os.environ["CF_ACCESS_CLIENT_SECRET"],
}

def post(path, payload, extra=None, raw=None):
    request_headers = {**headers, **(extra or {})}
    if raw is None:
        response = requests.post(base_url + path, json=payload, headers=request_headers, timeout=30)
    else:
        request_headers["Content-Type"] = "application/json"
        response = requests.post(base_url + path, data=raw, headers=request_headers, timeout=30)
    response.raise_for_status()
    return response.json()["data"]

private_key = Ed25519PrivateKey.generate()
public_key = private_key.public_key().public_bytes(
    serialization.Encoding.Raw, serialization.PublicFormat.Raw
)
enrollment = post("/api/v1/remote-ingest/enroll", {
    "registration_token": os.environ["REGISTRATION_TOKEN"],
    "machine_name": socket.gethostname(),
    "public_key": base64.b64encode(public_key).decode(),
})
client_id = enrollment["client_id"]
challenge = post("/api/v1/remote-ingest/handshakes", {"client_id": client_id})

payload = {
    "external_id": f"remote-{int(time.time())}", "name": "remote-openai",
    "platform": "openai", "base_url": os.environ["REMOTE_BASE_URL"],
    "api_key": os.environ["REMOTE_API_KEY"], "group_name": os.environ["REMOTE_GROUP_NAME"],
    "test_model": "gpt-4.1-mini", "concurrency": 1, "priority": 0, "rate_multiplier": 1,
}
body = json.dumps(payload, separators=(",", ":")).encode()
timestamp = str(int(time.time()))
canonical = "\n".join([
    "sub2api-remote-ingest-v1", client_id, challenge["challenge_id"], challenge["nonce"],
    timestamp, hashlib.sha256(body).hexdigest(),
])
signature = base64.b64encode(private_key.sign(canonical.encode())).decode()
delivery = post("/api/v1/remote-ingest/accounts", None, {
    "X-Remote-Client-Id": client_id,
    "X-Remote-Challenge-Id": challenge["challenge_id"],
    "X-Remote-Timestamp": timestamp,
    "X-Remote-Signature": signature,
}, body)
print(json.dumps(delivery))
