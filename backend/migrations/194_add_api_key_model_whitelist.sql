-- Add an optional per-key model allowlist.
-- NULL or an empty array preserves the legacy unrestricted behavior.
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS model_whitelist JSONB DEFAULT NULL;

COMMENT ON COLUMN api_keys.model_whitelist IS
    'Allowed request models for this API key; NULL or [] means all models';
