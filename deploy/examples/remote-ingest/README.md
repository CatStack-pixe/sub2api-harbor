# Remote Ingest Client Examples

All examples enroll one ephemeral Ed25519 key and submit one OpenAI account.
Set the following variables before running them:

```bash
export INGEST_URL=https://ingest.catpithos.top
export REGISTRATION_TOKEN='one-time-admin-token'
export CF_ACCESS_CLIENT_ID='service-token-id.access'
export CF_ACCESS_CLIENT_SECRET='service-token-secret'
export REMOTE_API_KEY='upstream-api-key'
export REMOTE_BASE_URL='https://api.openai.com/v1'
export REMOTE_GROUP_NAME='openai-default'
```

The private key, API key, and registration token must not be committed or
passed in command-line arguments. `curl.sh` uses `jq` and OpenSSL. The Python
example requires `requests` and `cryptography`; the JavaScript example needs
Node.js 18+; the Go example uses the standard library.

```bash
./curl.sh
python client.py
node client.mjs
go run client.go
```

For a production client, keep the generated Ed25519 private key in a protected
machine secret store after enrollment and reuse its returned `client_id`. Do
not automatically recreate identities after a failed enrollment: the
registration token can only be consumed once.

The complete client handoff and retry contract is documented in
[`../../REMOTE_INGEST_CLIENT_GUIDE.zh-CN.md`](../../REMOTE_INGEST_CLIENT_GUIDE.zh-CN.md).
