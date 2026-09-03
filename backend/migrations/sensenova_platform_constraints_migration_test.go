package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSenseNovaPlatformConstraintsMigration(t *testing.T) {
	content, err := FS.ReadFile("235_add_sensenova_platform_constraints.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	for _, table := range []string{"user_platform_quotas", "composite_model_routes"} {
		require.Contains(t, sql, table)
	}
	require.Contains(t, sql, "'sensenova'")
}
