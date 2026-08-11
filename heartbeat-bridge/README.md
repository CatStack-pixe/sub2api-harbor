# Heartbeat DeepSeek Bridge

This service accepts the external DeepSeek heartbeat at `POST /api/heartbeat`,
stores supported fingerprints durably, retrieves the matching key from the
configured vault, verifies its balance, then makes the account available in
Sub2API.

Each account is configured with pool mode, concurrency `3`, priority `50`, the
configured DeepSeek account group, and a randomly chosen proxy from the fastest
10 percent of the configured proxy group. Proxy measurements are shared for the
configured cache period so jobs do not re-probe the full proxy group.

The service refuses to create accounts when a `deepseek-default` group exists.
This prevents the current Sub2API create API from exposing an unverified account
through an implicit default group. It also validates the configured DeepSeek
group during startup and before each create.

Deploy it only as a Compose overlay:

```sh
docker compose -f docker-compose.yml -f docker-compose.heartbeat-bridge.yml up -d heartbeat-bridge
```

Set deployment-only values in `/opt/sub2api/.env`; do not commit them. The
documented vault protocol uses HTTP and query-string authentication. It requires
an explicit insecure setting and should be replaced with HTTPS, mTLS, or a
private tunnel when the upstream supports it.
