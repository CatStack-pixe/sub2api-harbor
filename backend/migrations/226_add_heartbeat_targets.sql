ALTER TABLE heartbeat_provision_jobs
    ADD COLUMN IF NOT EXISTS target_group_id BIGINT NULL REFERENCES groups(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS target_proxy_group_id BIGINT NULL REFERENCES proxy_groups(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS heartbeat_provision_jobs_target_group_idx
    ON heartbeat_provision_jobs (target_group_id, target_proxy_group_id);
