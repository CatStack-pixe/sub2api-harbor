CREATE TABLE IF NOT EXISTS heartbeat_provision_jobs (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    fingerprint CHAR(24) NOT NULL,
    session_key_ciphertext TEXT NOT NULL,
    source_balance DOUBLE PRECISION NULL,
    source_checked_at TIMESTAMPTZ NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lock_owner TEXT NULL,
    locked_until TIMESTAMPTZ NULL,
    account_id BIGINT NULL REFERENCES accounts(id) ON DELETE SET NULL,
    proxy_id BIGINT NULL REFERENCES proxies(id) ON DELETE SET NULL,
    last_error TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ NULL,
    CONSTRAINT heartbeat_provision_jobs_provider_fingerprint_unique UNIQUE (provider, fingerprint),
    CONSTRAINT heartbeat_provision_jobs_status_check CHECK (status IN ('queued', 'processing', 'retry', 'complete', 'failed'))
);

CREATE INDEX IF NOT EXISTS heartbeat_provision_jobs_ready_idx
    ON heartbeat_provision_jobs (available_at, id)
    WHERE status IN ('queued', 'retry');

CREATE INDEX IF NOT EXISTS heartbeat_provision_jobs_lease_idx
    ON heartbeat_provision_jobs (locked_until)
    WHERE status = 'processing';
