# Remote Account Intake

Remote account intake is disabled by default. It accepts one API-key account at
a time on the dedicated `ingest.catpithos.top` hostname. It must never share a
hostname, Cloudflare Access application, or service token with the normal API.

## Prerequisites

1. Create a Cloudflare Access self-hosted application for
   `ingest.catpithos.top/api/v1/remote-ingest/*`.
2. Create one Cloudflare Access **Service Token per remote machine** and allow
   only that token in the application policy. The client sends its service
   token to Cloudflare using `CF-Access-Client-Id` and
   `CF-Access-Client-Secret`; Cloudflare supplies the signed
   `Cf-Access-Jwt-Assertion` to the origin.
3. Configure the Access team hostname and this application's audience (AUD)
   in the remote-ingest Compose environment or `config.yaml`.
4. Restrict the origin firewall's TCP/443 inbound sources to the published
   Cloudflare IP ranges. Do not allow direct Internet access to this hostname.
5. Deploy the extra Caddy virtual host in `Caddyfile`; it exposes only the
   remote-ingest API and limits each request body to 16 KiB.
6. Set `BIND_HOST=127.0.0.1` and verify port 8080 is not reachable from another
   machine. Only Caddy should be able to reach the application origin.

Cloudflare Access configuration and origin JWT validation are described in
[Cloudflare service tokens](https://developers.cloudflare.com/cloudflare-one/access-controls/service-credentials/service-tokens/)
and [Cloudflare JWT validation](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/http-apps/authorization-cookie/validating-json/).

## Keyring and Compose

Create a host-only keyring file. Each value is an independent random 32-byte
AES key encoded using standard Base64.

```bash
umask 077
mkdir -p secrets
KEY=$(openssl rand -base64 32)
printf '{"active_key_id":"2026-08","keys":{"2026-08":"%s"}}\n' "$KEY" \
  > secrets/remote-ingest-keyring.json
chmod 0400 secrets/remote-ingest-keyring.json
```

Set `REMOTE_INGEST_CLOUDFLARE_TEAM_DOMAIN`,
`REMOTE_INGEST_CLOUDFLARE_AUDIENCE`, and
`REMOTE_INGEST_KEYRING_SOURCE` in `.env`, then start the normal stack with the
opt-in override:

```bash
docker compose -f docker-compose.local.yml \
  -f docker-compose.remote-ingest.yml up -d
```

For the named-volume setup, replace `docker-compose.local.yml` with
`docker-compose.yml`. The base files intentionally do not reference the
keyring secret, so disabled installations retain their existing startup path.

The application encrypts remote API keys as versioned AES-256-GCM envelopes
before writing PostgreSQL or Redis. To rotate, add a new 32-byte key, switch
`active_key_id`, restart the application, and retain the old key until all
records encrypted with it have been retired. Losing an old key makes matching
remote accounts unreadable.

## Operating Model

An administrator creates a short-lived, one-use registration token in
**Admin > Remote Ingest**. The remote client uses that token once to enroll an
Ed25519 public key and bind the Cloudflare Access identity. Each account
submission then requires a new 60-second nonce and an Ed25519 signature.

The account is committed as inactive, bound to an existing active group in the
same transaction, and asynchronously probed. It becomes schedulable only on a
successful probe. Failed probes stay inactive and can be retried by an
administrator. The registration token, delivery query token, and API key are
never shown in list views or logs.

The backend rejects HTTP, userinfo, query strings, fragments, loopback,
link-local, private, multicast, and reserved resolved destinations. Use
`REMOTE_INGEST_ALLOWED_PRIVATE_CIDRS` only for a reviewed internal upstream;
use narrowly scoped CIDRs, not broad private ranges.

## Client Protocol

The public endpoints are all under `/api/v1/remote-ingest`:

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/enroll` | Consume a registration token and bind the Ed25519 public key to the Access identity. |
| `POST` | `/handshakes` | Issue a single-use 32-byte nonce valid for 60 seconds. |
| `POST` | `/accounts` | Submit exactly one API-key account. |
| `GET` | `/deliveries/:id` | Read one delivery using its one-time-returned query token. |

`/accounts` signs the literal UTF-8 byte sequence below, with LF line endings:

```text
sub2api-remote-ingest-v1\n
<client_id>\n
<challenge_id>\n
<nonce>\n
<unix_timestamp_seconds>\n
<lowercase_sha256_of_exact_request_body>
```

Send the Base64 signature in `X-Remote-Signature`, along with
`X-Remote-Client-Id`, `X-Remote-Challenge-Id`, and `X-Remote-Timestamp`.
The delivery response is `202 Accepted` and contains `delivery_id` and a
read-only `query_token`. Use the query token only in `Authorization: Bearer`.

Runnable reference clients are under `deploy/examples/remote-ingest/`. They
need the registration token, a Cloudflare Access service token, and the API
key only in process environment variables. They print the delivery ID and
query token; redirect sensitive output to a protected terminal only.
