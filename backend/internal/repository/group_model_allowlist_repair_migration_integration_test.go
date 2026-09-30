//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestGroupModelAllowlistMigrationsPreserveIndependentForkPolicies(t *testing.T) {
	const legacyConfig = `{"enabled":true,"models":["public-alias"],"model_mapping_enabled":true,"model_mapping":{"public-alias":"agnes-2.5-pro-alpha"},"extra":{"keep":true}}`
	const currentAllowlist = `{"enabled":true,"models":["independent-model"]}`
	for _, migration := range []string{"235_group_model_allowlist.sql", "236_group_model_allowlist_repair.sql"} {
		for _, scenario := range []struct {
			name          string
			legacyColumn  bool
			allowColumn   bool
			allowlist     string
			wantAllowlist string
		}{
			{name: "legacy listing and mappings only", legacyColumn: true, wantAllowlist: `{}`},
			{name: "both independent policies", legacyColumn: true, allowColumn: true, allowlist: currentAllowlist, wantAllowlist: currentAllowlist},
			{name: "disabled allowlist remains disabled", legacyColumn: true, allowColumn: true, allowlist: `{}`, wantAllowlist: `{}`},
			{name: "null allowlist repaired without enabling", legacyColumn: true, allowColumn: true, wantAllowlist: `{}`},
			{name: "allowlist only", allowColumn: true, allowlist: currentAllowlist, wantAllowlist: currentAllowlist},
			{name: "both columns absent", wantAllowlist: `{}`},
		} {
			t.Run(migration+"/"+scenario.name, func(t *testing.T) {
				tx := testTx(t)
				ctx := context.Background()
				// A temporary table shadows public.groups and verifies search_path
				// handling without modifying the integration fixture's schema.
				_, err := tx.ExecContext(ctx, "CREATE TEMP TABLE groups (id BIGINT PRIMARY KEY) ON COMMIT DROP")
				require.NoError(t, err)
				_, err = tx.ExecContext(ctx, "INSERT INTO groups (id) VALUES (1)")
				require.NoError(t, err)
				legacyBefore := "{}"
				if scenario.legacyColumn {
					_, err = tx.ExecContext(ctx, "ALTER TABLE groups ADD COLUMN models_list_config JSONB")
					require.NoError(t, err)
					_, err = tx.ExecContext(ctx, "UPDATE groups SET models_list_config = $1::jsonb WHERE id = 1", legacyConfig)
					require.NoError(t, err)
					require.NoError(t, tx.QueryRowContext(ctx, "SELECT models_list_config::text FROM groups WHERE id = 1").Scan(&legacyBefore))
				}
				if scenario.allowColumn {
					_, err = tx.ExecContext(ctx, `ALTER TABLE groups ADD COLUMN model_allowlist JSONB DEFAULT '{"enabled":true,"models":["unexpected-default"]}'::jsonb`)
					require.NoError(t, err)
					if scenario.allowlist == "" {
						_, err = tx.ExecContext(ctx, "UPDATE groups SET model_allowlist = NULL WHERE id = 1")
					} else {
						_, err = tx.ExecContext(ctx, "UPDATE groups SET model_allowlist = $1::jsonb WHERE id = 1", scenario.allowlist)
					}
					require.NoError(t, err)
				}

				content, err := dbmigrations.FS.ReadFile(migration)
				require.NoError(t, err)
				for replay := 0; replay < 2; replay++ {
					_, err = tx.ExecContext(ctx, string(content))
					require.NoError(t, err)
					var listing, allowlist string
					require.NoError(t, tx.QueryRowContext(ctx, "SELECT models_list_config::text, model_allowlist::text FROM groups WHERE id = 1").Scan(&listing, &allowlist))
					require.Equal(t, legacyBefore, listing, "stored legacy JSON must remain unchanged")
					require.JSONEq(t, scenario.wantAllowlist, allowlist)
					requireModelAllowlistColumnShape(ctx, t, tx)
				}

				// Even an existing unsafe default is reset to a disabled policy.
				var newAllowlist string
				require.NoError(t, tx.QueryRowContext(ctx, "INSERT INTO groups (id) VALUES (2) RETURNING model_allowlist::text").Scan(&newAllowlist))
				require.JSONEq(t, `{}`, newAllowlist)
			})
		}
	}
}

func requireModelAllowlistColumnShape(ctx context.Context, t *testing.T, tx *sql.Tx) {
	t.Helper()

	var notNull bool
	var columnDefault string
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT a.attnotnull, pg_get_expr(d.adbin, d.adrelid)
FROM pg_attribute a
JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE a.attrelid = 'groups'::regclass AND a.attname = 'model_allowlist'
`).Scan(&notNull, &columnDefault))
	require.True(t, notNull)
	require.Contains(t, columnDefault, "'{}'::jsonb")
}
