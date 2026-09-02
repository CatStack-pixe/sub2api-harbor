-- Channel Monitor V2 selective aggregation indexes.
--
-- This migration is non-transactional because the indexes are built
-- concurrently on production-sized usage and ops tables. The aggregator
-- constrains both dimensions by a bounded created_at window; keeping the
-- time column first supports the watermark scan, while request_id/status
-- covers the error candidate set and final association.

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_created_at_group_id
    ON usage_logs (created_at DESC, group_id)
    WHERE group_id IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ops_error_logs_created_request_status
    ON ops_error_logs (created_at DESC, request_id, status_code)
    WHERE request_id IS NOT NULL AND NULLIF(request_id, '') IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ops_error_logs_request_created_status
    ON ops_error_logs (request_id, created_at DESC, status_code)
    WHERE request_id IS NOT NULL AND NULLIF(request_id, '') IS NOT NULL;
