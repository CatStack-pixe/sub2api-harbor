//go:build integration

package repository

import (
	"context"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration222DefaultsExistingGroupsToDisabled(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	migrationSQL, err := dbmigrations.FS.ReadFile("222_group_global_prompt.sql")
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
VALUES ('migration-222-existing')
RETURNING id
`).Scan(&groupID))

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	var enabled bool
	var prompt string
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT global_prompt_enabled, global_prompt
FROM groups
WHERE id = $1
`, groupID).Scan(&enabled, &prompt))
	require.False(t, enabled)
	require.Empty(t, prompt)

	_, err = tx.ExecContext(ctx, `
UPDATE groups
SET global_prompt_enabled = TRUE, global_prompt = 'saved policy'
WHERE id = $1
`, groupID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT global_prompt_enabled, global_prompt
FROM groups
WHERE id = $1
`, groupID).Scan(&enabled, &prompt))
	require.True(t, enabled)
	require.Equal(t, "saved policy", prompt)
}
