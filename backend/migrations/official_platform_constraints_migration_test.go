package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfficialPlatformConstraintsMigration(t *testing.T) {
	content, err := FS.ReadFile("230_add_official_platform_constraints.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	sql := strings.Join(strings.Fields(string(content)), " ")
	for _, table := range []string{"user_platform_quotas", "composite_model_routes"} {
		require.Contains(t, sql, table)
	}
	for _, platform := range []string{"modelscope", "dashscope", "minimax", "volcengine"} {
		require.Contains(t, sql, "'"+platform+"'")
	}
}
