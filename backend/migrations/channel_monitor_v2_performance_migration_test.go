package migrations

import (
	"strings"
	"testing"
)

func TestChannelMonitorV2PerformanceMigrationAddsSelectiveIndexes(t *testing.T) {
	data, err := FS.ReadFile("234_channel_monitor_v2_performance_notx.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(data)
	for _, want := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_created_at_group_id",
		"ON usage_logs (created_at DESC, group_id)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ops_error_logs_created_request_status",
		"ON ops_error_logs (created_at DESC, request_id, status_code)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ops_error_logs_request_created_status",
		"ON ops_error_logs (request_id, created_at DESC, status_code)",
		"NULLIF(request_id, '') IS NOT NULL",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("migration missing %q", want)
		}
	}
}
