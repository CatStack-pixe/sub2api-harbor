//go:build integration

package repository

import (
	"context"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration221EnablesLongContextPricingForExistingGroups(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	migrationSQL, err := dbmigrations.FS.ReadFile("221_group_model_pricing.sql")
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx, `
CREATE TEMPORARY TABLE groups (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL
)
`)
	require.NoError(t, err)
	var groupID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name)
VALUES ('migration-221-existing')
RETURNING id
`).Scan(&groupID))

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	var enabled bool
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT long_context_pricing_enabled
FROM groups
WHERE id = $1
`, groupID).Scan(&enabled))
	require.True(t, enabled)

	_, err = tx.ExecContext(ctx, `
UPDATE groups
SET long_context_pricing_enabled = FALSE
WHERE id = $1
`, groupID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT long_context_pricing_enabled
FROM groups
WHERE id = $1
`, groupID).Scan(&enabled))
	require.True(t, enabled)
}
