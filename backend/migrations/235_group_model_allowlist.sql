-- Keep the fork's model listing and alias mapping configuration independent
-- from the request allowlist. Existing display settings must never enable
-- request admission restrictions during an upgrade or partial schema repair.
-- Direct table references follow search_path. Existing JSON values are retained.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS models_list_config JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS model_allowlist JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Repair incomplete new-column definitions without changing existing policies.
UPDATE groups SET model_allowlist = '{}'::jsonb WHERE model_allowlist IS NULL;

ALTER TABLE groups ALTER COLUMN model_allowlist SET DEFAULT '{}'::jsonb;
ALTER TABLE groups ALTER COLUMN model_allowlist SET NOT NULL;

COMMENT ON COLUMN groups.model_allowlist IS
    'Independent group model allowlist: disabled by default; constrains model listing responses and request admission when enabled';
