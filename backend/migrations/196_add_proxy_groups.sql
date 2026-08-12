CREATE TABLE IF NOT EXISTS proxy_groups (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT proxy_groups_name_not_blank
        CHECK (CHAR_LENGTH(BTRIM(name)) BETWEEN 1 AND 100)
);

CREATE UNIQUE INDEX IF NOT EXISTS proxy_groups_name_unique_ci
    ON proxy_groups ((LOWER(BTRIM(name))));

ALTER TABLE proxies
    ADD COLUMN IF NOT EXISTS proxy_group_id BIGINT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.table_constraints
        WHERE constraint_schema = 'public'
          AND table_name = 'proxies'
          AND constraint_name = 'proxies_proxy_group_id_fkey'
    ) THEN
        ALTER TABLE proxies
            ADD CONSTRAINT proxies_proxy_group_id_fkey
            FOREIGN KEY (proxy_group_id)
            REFERENCES proxy_groups(id)
            ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS proxies_proxy_group_id_idx
    ON proxies (proxy_group_id);
