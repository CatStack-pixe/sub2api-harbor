-- Persist explicit client session identifiers for error-request correlation.
ALTER TABLE ops_error_logs
  ADD COLUMN IF NOT EXISTS session_id VARCHAR(255);

CREATE INDEX IF NOT EXISTS idx_ops_error_logs_session_time
  ON ops_error_logs (session_id, created_at DESC)
  WHERE session_id IS NOT NULL;

COMMENT ON COLUMN ops_error_logs.session_id IS
  'Sanitized explicit client session identifier for request troubleshooting; NULL when absent or invalid.';
