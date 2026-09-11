package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorSenseNovaMigration(t *testing.T) {
	content, err := FS.ReadFile("236_channel_monitor_sensenova.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "channel_monitors_provider_check")
	require.Contains(t, sql, "channel_monitor_request_templates_provider_check")
	require.Contains(t, sql, "'deepseek', 'sensenova'")
	require.Contains(t, sql, "position('sensenova' IN monitor_constraint_def) = 0")
	require.Contains(t, sql, "position('sensenova' IN template_constraint_def) = 0")
}
