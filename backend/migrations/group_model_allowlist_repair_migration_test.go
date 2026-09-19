package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupModelAllowlistRepairMigration(t *testing.T) {
	for _, name := range []string{"235_group_model_allowlist.sql", "236_group_model_allowlist_repair.sql"} {
		t.Run(name, func(t *testing.T) {
			content, err := FS.ReadFile(name)
			require.NoError(t, err)
			sql := strings.Join(strings.Fields(string(content)), " ")
			require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS models_list_config JSONB NOT NULL DEFAULT '{}'::jsonb")
			require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS model_allowlist JSONB NOT NULL DEFAULT '{}'::jsonb")
			require.Contains(t, sql, "UPDATE groups SET model_allowlist = '{}'::jsonb WHERE model_allowlist IS NULL")
			require.Contains(t, sql, "ALTER TABLE groups ALTER COLUMN model_allowlist SET DEFAULT '{}'::jsonb")
			require.Contains(t, sql, "ALTER TABLE groups ALTER COLUMN model_allowlist SET NOT NULL")
			require.NotContains(t, sql, "RENAME COLUMN")
			require.NotContains(t, sql, "SET model_allowlist = models_list_config")
			require.NotContains(t, sql, "SET models_list_config =")
			require.NotContains(t, sql, "DROP COLUMN")
			require.NotContains(t, sql, "table_schema = 'public'")
		})
	}
}
