CREATE TABLE IF NOT EXISTS remote_ingest_registration_tokens (
    id UUID PRIMARY KEY,
    token_hash BYTEA NOT NULL UNIQUE,
    token_fingerprint VARCHAR(16) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    used_by_client_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS remote_ingest_clients (
    id UUID PRIMARY KEY,
    machine_name VARCHAR(100) NOT NULL,
    public_key BYTEA NOT NULL,
    public_key_fingerprint VARCHAR(64) NOT NULL UNIQUE,
    access_subject VARCHAR(255) NOT NULL UNIQUE,
    enrolled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_active_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'remote_ingest_registration_tokens_used_by_client_fkey'
    ) THEN
        ALTER TABLE remote_ingest_registration_tokens
            ADD CONSTRAINT remote_ingest_registration_tokens_used_by_client_fkey
            FOREIGN KEY (used_by_client_id) REFERENCES remote_ingest_clients(id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS remote_ingest_deliveries (
    id UUID PRIMARY KEY,
    client_id UUID NOT NULL REFERENCES remote_ingest_clients(id) ON DELETE RESTRICT,
    external_id VARCHAR(128) NOT NULL,
    payload_hash BYTEA NOT NULL,
    query_token_hash BYTEA NOT NULL UNIQUE,
    query_token_ciphertext TEXT NOT NULL,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    platform VARCHAR(50) NOT NULL,
    group_name VARCHAR(100) NOT NULL,
    test_model VARCHAR(200),
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    masked_error TEXT,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT remote_ingest_deliveries_status_check
        CHECK (status IN ('pending', 'probing', 'active', 'probe_failed')),
    CONSTRAINT remote_ingest_deliveries_client_external_unique UNIQUE (client_id, external_id)
);

CREATE INDEX IF NOT EXISTS idx_remote_ingest_tokens_expires_at
    ON remote_ingest_registration_tokens (expires_at);
CREATE INDEX IF NOT EXISTS idx_remote_ingest_clients_last_active
    ON remote_ingest_clients (last_active_at DESC);
CREATE INDEX IF NOT EXISTS idx_remote_ingest_deliveries_claim
    ON remote_ingest_deliveries (next_attempt_at, created_at)
    WHERE status IN ('pending', 'probing');
CREATE INDEX IF NOT EXISTS idx_remote_ingest_deliveries_client_created
    ON remote_ingest_deliveries (client_id, created_at DESC);
