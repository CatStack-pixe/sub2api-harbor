package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupModelPricingMigrationPreservesLongContextBilling(t *testing.T) {
	content, err := FS.ReadFile("221_group_model_pricing.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS long_context_pricing_enabled BOOLEAN NOT NULL DEFAULT TRUE")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS model_pricing JSONB")
	require.Contains(t, sql, "UPDATE groups SET long_context_pricing_enabled = TRUE")
	require.Contains(t, sql, "WHERE long_context_pricing_enabled IS DISTINCT FROM TRUE")
}
