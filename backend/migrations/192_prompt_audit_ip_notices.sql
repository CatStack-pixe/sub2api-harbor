-- Store the security-resolved source IP on prompt audit records and support
-- one-time administrator notices delivered on the target IP's next API call.
ALTER TABLE prompt_audit_jobs
    ADD COLUMN IF NOT EXISTS client_ip VARCHAR(45) NOT NULL DEFAULT '';

ALTER TABLE prompt_audit_events
    ADD COLUMN IF NOT EXISTS client_ip VARCHAR(45) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_client_ip_created
    ON prompt_audit_events(client_ip, created_at DESC, id DESC)
    WHERE client_ip <> '';

CREATE TABLE IF NOT EXISTS prompt_audit_ip_notices (
    id                BIGSERIAL PRIMARY KEY,
    source_event_id   BIGINT REFERENCES prompt_audit_events(id) ON DELETE SET NULL,
    client_ip         VARCHAR(45) NOT NULL,
    message           VARCHAR(1000) NOT NULL,
    created_by        BIGINT REFERENCES users(id) ON DELETE SET NULL,
    status            VARCHAR(16) NOT NULL DEFAULT 'pending',
    delivered_request_id VARCHAR(128) NOT NULL DEFAULT '',
    expires_at        TIMESTAMPTZ NOT NULL,
    delivered_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_prompt_audit_ip_notices_status
        CHECK (status IN ('pending', 'delivered', 'expired')),
    CONSTRAINT chk_prompt_audit_ip_notices_client_ip
        CHECK (client_ip <> ''),
    CONSTRAINT chk_prompt_audit_ip_notices_message
        CHECK (message <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_prompt_audit_ip_notices_pending_ip
    ON prompt_audit_ip_notices(client_ip)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_prompt_audit_ip_notices_expiry
    ON prompt_audit_ip_notices(status, expires_at, id);
