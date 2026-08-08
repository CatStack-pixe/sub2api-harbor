#!/usr/bin/env bash
set -euo pipefail

: "${INGEST_URL:?} ${REGISTRATION_TOKEN:?} ${CF_ACCESS_CLIENT_ID:?} ${CF_ACCESS_CLIENT_SECRET:?}"
: "${REMOTE_API_KEY:?} ${REMOTE_BASE_URL:?} ${REMOTE_GROUP_NAME:?}"
command -v jq >/dev/null
command -v openssl >/dev/null

umask 077
workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

headers=(-H "CF-Access-Client-Id: $CF_ACCESS_CLIENT_ID" -H "CF-Access-Client-Secret: $CF_ACCESS_CLIENT_SECRET")
openssl genpkey -algorithm Ed25519 -out "$workdir/private.pem"
public_key=$(openssl pkey -in "$workdir/private.pem" -pubout -outform DER | tail -c 32 | base64 | tr -d '\n')

enroll=$(jq -cn --arg token "$REGISTRATION_TOKEN" --arg machine "$(hostname)" --arg key "$public_key" \
  '{registration_token:$token,machine_name:$machine,public_key:$key}')
client_id=$(curl -fsS "${headers[@]}" -H 'Content-Type: application/json' \
  -d "$enroll" "$INGEST_URL/api/v1/remote-ingest/enroll" | jq -r '.data.client_id')

challenge=$(curl -fsS "${headers[@]}" -H 'Content-Type: application/json' \
  -d "{\"client_id\":\"$client_id\"}" "$INGEST_URL/api/v1/remote-ingest/handshakes")
challenge_id=$(jq -r '.data.challenge_id' <<<"$challenge")
nonce=$(jq -r '.data.nonce' <<<"$challenge")

jq -cn --arg external "remote-$(date +%s)" --arg key "$REMOTE_API_KEY" --arg base "$REMOTE_BASE_URL" --arg group "$REMOTE_GROUP_NAME" \
  '{external_id:$external,name:"remote-openai",platform:"openai",base_url:$base,api_key:$key,group_name:$group,test_model:"gpt-4.1-mini",concurrency:1,priority:0,rate_multiplier:1}' \
  > "$workdir/payload.json"
timestamp=$(date +%s)
body_hash=$(sha256sum "$workdir/payload.json" | awk '{print $1}')
canonical=$(printf 'sub2api-remote-ingest-v1\n%s\n%s\n%s\n%s\n%s' "$client_id" "$challenge_id" "$nonce" "$timestamp" "$body_hash")
signature=$(printf %s "$canonical" | openssl pkeyutl -sign -rawin -inkey "$workdir/private.pem" | base64 | tr -d '\n')

curl -fsS "${headers[@]}" -H 'Content-Type: application/json' \
  -H "X-Remote-Client-Id: $client_id" \
  -H "X-Remote-Challenge-Id: $challenge_id" \
  -H "X-Remote-Timestamp: $timestamp" \
  -H "X-Remote-Signature: $signature" \
  --data-binary "@$workdir/payload.json" "$INGEST_URL/api/v1/remote-ingest/accounts"
