#!/bin/sh
set -eu

docker exec -i sub2api-postgres psql -U sub2api -d sub2api -v ON_ERROR_STOP=1 <<'SQL'
BEGIN;

CREATE TEMP TABLE prompt_audit_deleted_jobs (
    job_id BIGINT PRIMARY KEY
) ON COMMIT DROP;

WITH count_cutoff AS (
    SELECT id
    FROM prompt_audit_events
    ORDER BY id DESC
    OFFSET 9999
    LIMIT 1
), deleted AS (
    DELETE FROM prompt_audit_events e
    WHERE e.created_at < NOW() - INTERVAL '24 hours'
       OR EXISTS (
            SELECT 1
            FROM count_cutoff c
            WHERE e.id < c.id
       )
    RETURNING e.job_id
)
INSERT INTO prompt_audit_deleted_jobs(job_id)
SELECT DISTINCT job_id
FROM deleted
ON CONFLICT DO NOTHING;

DELETE FROM prompt_audit_jobs j
USING prompt_audit_deleted_jobs d
WHERE j.id = d.job_id
  AND j.status <> 'processing'
  AND NOT EXISTS (
      SELECT 1
      FROM prompt_audit_events e
      WHERE e.job_id = j.id
  );

COMMIT;

SELECT
    count(*) AS retained_events,
    pg_size_pretty(pg_total_relation_size('prompt_audit_events')) AS event_table_size
FROM prompt_audit_events;
SQL
