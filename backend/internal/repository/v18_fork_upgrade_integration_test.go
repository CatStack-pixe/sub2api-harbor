//go:build integration

package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestV18UpgradePreservesHarborRowsAndAppliesOnlyNewDefaults(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	// Reconstruct the deployed v0.2.17 schema in a rollback-only transaction.
	_, err := tx.ExecContext(ctx, "DROP TABLE scoped_admin_api_keys; ALTER TABLE groups DROP COLUMN image_count_billing")
	require.NoError(t, err)
	history := v16MigrationHistory(t, ctx, tx)
	var userID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO users(email,password_hash,balance) VALUES('v18-history@example.test','fixture',12.34) RETURNING id`).Scan(&userID))
	groupIDs := make(map[string]int64)
	for _, platform := range []string{"gemini", "antigravity", "composite", "openai", "kimi", "cline"} {
		var id int64
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups(name,platform,allow_image_generation,models_list_config,model_pricing)
VALUES($1,$2,false,'{"enabled":true,"models":["harbor-public"]}',
'[{"model":"harbor-public","input_price_per_million":20}]') RETURNING id`,
			"v18-harbor-"+platform, platform).Scan(&id))
		groupIDs[platform] = id
	}
	snapshot := func() string {
		var value string
		require.NoError(t, tx.QueryRowContext(ctx, `
SELECT coalesce(jsonb_agg(to_jsonb(g)-'allow_image_generation'-'image_count_billing' ORDER BY id),'[]'::jsonb)::text
FROM groups g WHERE name LIKE 'v18-harbor-%'`).Scan(&value))
		return value
	}
	before := snapshot()
	for _, name := range []string{
		"246_enable_gemini_image_generation_groups.sql",
		"247_scoped_admin_api_keys.sql",
		"248_group_image_count_billing.sql",
	} {
		v16ExecuteMigration(t, ctx, tx, name)
	}
	require.JSONEq(t, before, snapshot(), "pricing, aliases and every unrelated saved group field must survive")
	for platform, id := range groupIDs {
		var enabled, byCount bool
		require.NoError(t, tx.QueryRowContext(ctx, "SELECT allow_image_generation,image_count_billing FROM groups WHERE id=$1", id).Scan(&enabled, &byCount))
		require.Equal(t, platform == "gemini" || platform == "antigravity", enabled, platform)
		require.False(t, byCount, "existing billing policy must remain opt-in: %s", platform)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO scoped_admin_api_keys(id,name,key_prefix,key_hash,created_by,policy)
VALUES('v18-scoped-fixture','Historical scope','adm_sc_fixture',$1,$2,
'{"global_permissions":["groups.read"]}')`, strings.Repeat("a", 64), userID)
	require.NoError(t, err)
	var hash, policy string
	var deleted bool
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT key_hash,policy::text,deleted_at IS NULL FROM scoped_admin_api_keys WHERE id='v18-scoped-fixture'`).Scan(&hash, &policy, &deleted))
	require.Equal(t, strings.Repeat("a", 64), hash)
	require.JSONEq(t, `{"global_permissions":["groups.read"]}`, policy)
	require.True(t, deleted)
	_, err = tx.ExecContext(ctx, "UPDATE scoped_admin_api_keys SET revoked_at=now(),deleted_at=now() WHERE id='v18-scoped-fixture'")
	require.NoError(t, err)
	var retained int
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT count(*) FROM scoped_admin_api_keys WHERE id='v18-scoped-fixture' AND revoked_at IS NOT NULL AND deleted_at IS NOT NULL").Scan(&retained))
	require.Equal(t, 1, retained, "revocation must retain the audit identity")
	require.Equal(t, history, v16MigrationHistory(t, ctx, tx), "upgrade SQL must not rewrite prior migration records")
	var nullable string
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT is_nullable FROM information_schema.columns WHERE table_schema=current_schema()
AND table_name='groups' AND column_name='image_count_billing'`).Scan(&nullable))
	require.Equal(t, "NO", nullable)
}
