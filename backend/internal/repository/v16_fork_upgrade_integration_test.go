//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"sort"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Keep the deployed catalog independent of the implementation under test.
var v16HistoricalHarborPlatforms = []string{
	"anthropic", "openai", "gemini", "antigravity", "grok", "agnes",
	"deepseek", "nvidia", "tokenrhythm", "kimi", "zhipu", "chatanywhere",
	"glm", "modelscope", "dashscope", "minimax", "volcengine", "sensenova",
	"opencode_go", "tierflow", "senseaudio", "typesafe", "command_code", "cline",
}

func TestV16MigrationRunnerRetainsHistoryAndRejectsChanged242(t *testing.T) {
	ctx := context.Background()
	before := v16MigrationHistory(t, ctx, integrationDB)
	require.NoError(t, ApplyMigrations(ctx, integrationDB), "restart must verify the entire embedded migration chain")
	require.Equal(t, before, v16MigrationHistory(t, ctx, integrationDB), "restart must preserve checksums and application timestamps")

	// Exercise the production checksum gate on the original fork 242, rather
	// than merely comparing checksums generated from current files.
	const name = "242_drop_platform_check_constraints.sql"
	content, err := dbmigrations.FS.ReadFile(name)
	require.NoError(t, err)
	changed := fstest.MapFS{name: {Data: append(content, []byte("\n-- changed deployed migration\n")...)}}
	require.ErrorContains(t, applyMigrationsFS(ctx, integrationDB, changed), "migration "+name+" checksum mismatch")
	require.Equal(t, before, v16MigrationHistory(t, ctx, integrationDB), "a rejected edit must not rewrite migration history")
}

func TestV16ForkUpgradePreservesHistoricalRowsAndSchema(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	require.Subset(t, domain.ConcretePlatformIDs(), v16HistoricalHarborPlatforms)

	// TestMain has already applied the full production migration chain. Undo
	// only the four new schema changes inside this rollback-only transaction,
	// so the upgrade runs against the real deployed Harbor schema and data.
	_, err := tx.ExecContext(ctx, "DROP TABLE gemini_cached_contents")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "ALTER TABLE groups DROP COLUMN i18n")
	require.NoError(t, err)
	historyBefore := v16MigrationHistory(t, ctx, tx)
	constraintsBefore := v16LegacyConstraints(t, ctx, tx)
	indexesBefore := v16LegacyIndexes(t, ctx, tx)

	var userID, keyID, compositeID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO users (email, password_hash, balance)
VALUES ('v16-historical-harbor@example.test', 'fixture-hash', 123.456)
RETURNING id`).Scan(&userID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name, platform, description)
VALUES ('v16-historical-composite', 'composite', 'saved composite configuration')
RETURNING id`).Scan(&compositeID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO api_keys (user_id, key, name, group_id)
VALUES ($1, 'sk-v16-historical-harbor', 'historical key', $2)
RETURNING id`, userID, compositeID).Scan(&keyID))

	var firstGroupID, firstAccountID, cacheGroupID, cacheAccountID int64
	for _, platform := range v16HistoricalHarborPlatforms {
		var groupID, accountID int64
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (
 name, description, platform, rate_multiplier, models_list_config, model_allowlist,
 codex_models_manifest_config, global_prompt_enabled, global_prompt,
 max_reasoning_effort, max_reasoning_effort_over_limit, reasoning_effort_mappings,
 force_openai_fast, free_openai_fast, profit_control_enabled, profit_min_margin,
 allow_messages_dispatch, default_mapped_model, messages_dispatch_model_config, model_pricing)
VALUES ($1, 'saved Harbor description', $2, 1.2345,
 '{"enabled":true,"models":["harbor-public"],"model_mapping_enabled":true,"model_mapping":{"harbor-public":"harbor-upstream"}}',
 '{"enabled":true,"models":["independent-allowed"]}',
 '{"enabled":true,"account_ids":[123],"fallback_to_scheduler":false}',
 TRUE, 'saved Harbor policy', 'high', 'reject', '[{"from":"xhigh","to":"high"}]',
 TRUE, TRUE, TRUE, 0.15, TRUE, 'harbor-upstream',
 '{"exact_model_mappings":{"harbor-public":"harbor-upstream"}}',
 '[{"model":"harbor-public","input_price_per_million":2.5}]')
RETURNING id`, "v16-historical-"+platform, platform).Scan(&groupID), platform)
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, credentials, extra)
VALUES ($1, $2, 'apikey',
 '{"api_key":"fixture-only","model_mapping":{"harbor-public":"harbor-upstream"}}',
 '{"codex_fingerprint_mode":"full","codex_fingerprint_seed":"saved-harbor-seed","upstream_request_id_header":"X-Harbor-Trace","custom_models":["harbor-public"],"native_responses":false}')
RETURNING id`, "v16-historical-"+platform, platform).Scan(&accountID), platform)
		_, err = tx.ExecContext(ctx, `
UPDATE groups SET codex_models_manifest_config =
 jsonb_build_object('enabled', TRUE, 'account_ids', jsonb_build_array($2::bigint), 'fallback_to_scheduler', FALSE)
WHERE id = $1`, groupID, accountID)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, `
INSERT INTO account_groups (account_id, group_id, priority) VALUES ($1, $2, 37)`, accountID, groupID)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, `
INSERT INTO user_platform_quotas (
 user_id, platform, daily_limit_usd, weekly_limit_usd, monthly_limit_usd,
 daily_usage_usd, weekly_usage_usd, monthly_usage_usd, daily_window_start)
VALUES ($1, $2, 10, 50, 100, 1.25, 2.5, 3.75, '2026-10-13T00:00:00Z')`, userID, platform)
		require.NoError(t, err, platform)
		_, err = tx.ExecContext(ctx, `
INSERT INTO composite_model_routes (group_id, public_model, target_platform, upstream_model, endpoint, priority, notes)
VALUES ($1, $2, $3, 'harbor-upstream', 'responses', 37, 'saved routing policy')`, compositeID, platform+"-public", platform)
		require.NoError(t, err, platform)
		if platform == "openai" {
			firstGroupID, firstAccountID = groupID, accountID
		}
		if platform == "gemini" {
			cacheGroupID, cacheAccountID = groupID, accountID
		}
	}

	// Include retained monitor snapshots and histories for every implemented
	// provider. These CHECKs represent probe capabilities, not the catalog.
	for _, provider := range []string{
		"openai", "anthropic", "gemini", "grok", "antigravity", "kimi",
		"zhipu", "deepseek", "sensenova", "minimax", "opencode_go", "tierflow",
	} {
		var templateID, monitorID int64
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO channel_monitor_request_templates (name, provider, extra_headers, body_override_mode, body_override)
VALUES ($1, $2, '{"X-Harbor":"saved"}', 'merge', '{"stream":false}')
RETURNING id`, "v16-historical-"+provider, provider).Scan(&templateID))
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO channel_monitors (
 name, provider, endpoint, api_key_encrypted, primary_model, interval_seconds,
 created_by, template_id, extra_headers, body_override_mode, body_override)
VALUES ($1, $2, 'https://monitor.example.test', 'fixture-ciphertext', 'harbor-public',
 60, $3, $4, '{"X-Harbor":"saved"}', 'merge', '{"stream":false}')
RETURNING id`, "v16-historical-"+provider, provider, userID, templateID).Scan(&monitorID))
		_, err = tx.ExecContext(ctx, `
INSERT INTO channel_monitor_histories (monitor_id, model, status, latency_ms, message)
VALUES ($1, 'harbor-public', 'operational', 125, 'saved historical result')`, monitorID)
		require.NoError(t, err)
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO usage_logs (
 user_id, api_key_id, account_id, group_id, request_id, session_id, upstream_request_id,
 model, requested_model, upstream_model, upstream_response_model, upstream_model_mismatch,
 reasoning_effort, requested_reasoning_effort, native_compaction_v2, input_tokens, output_tokens,
 total_cost, actual_cost, inbound_endpoint, upstream_endpoint)
VALUES ($1, $2, $3, $4, 'v16-harbor-trace', 'v16-harbor-session', 'v16-upstream-trace',
 'harbor-public', 'harbor-public', 'harbor-upstream', 'harbor-response-model', TRUE,
 'high', 'xhigh', TRUE, 1200, 350, 0.125, 0.0625, '/v1/responses', '/v1/chat/completions')`,
		userID, keyID, firstAccountID, firstGroupID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
INSERT INTO ops_error_logs (
 user_id, api_key_id, account_id, group_id, request_id, client_request_id,
 session_id, upstream_errors, platform, model, error_phase, error_type, error_message)
VALUES ($1, $2, $3, $4, 'v16-harbor-trace', 'v16-client-trace',
 'v16-harbor-session', '[{"request_id":"v16-upstream-trace","status_code":502}]', 'openai', 'harbor-public',
 'upstream', 'upstream_error', 'saved diagnostic detail')`, userID, keyID, firstAccountID, firstGroupID)
	require.NoError(t, err)

	tables := []string{
		"users", "groups", "accounts", "api_keys", "account_groups", "user_platform_quotas",
		"composite_model_routes", "usage_logs", "ops_error_logs", "channel_monitors",
		"channel_monitor_request_templates", "channel_monitor_histories",
	}
	snapshots := make(map[string]string, len(tables))
	for _, table := range tables {
		snapshots[table] = v16RowsSnapshot(t, ctx, tx, table)
	}

	names, err := fs.Glob(dbmigrations.FS, "*.sql")
	require.NoError(t, err)
	sort.Strings(names)
	var applied []string
	for _, name := range names {
		if name <= "242_drop_platform_check_constraints.sql" || name > "245_gemini_cached_contents_mutation_lease.sql" {
			continue
		}
		v16ExecuteMigration(t, ctx, tx, name)
		applied = append(applied, name)
	}
	require.Equal(t, []string{
		"242_group_i18n.sql", "243_gemini_cached_contents.sql",
		"244_gemini_cached_contents_channel_usage.sql", "245_gemini_cached_contents_mutation_lease.sql",
	}, applied)
	for _, table := range tables {
		require.Equal(t, snapshots[table], v16RowsSnapshot(t, ctx, tx, table), "every saved field and row in %s must survive", table)
	}
	require.Equal(t, constraintsBefore, v16LegacyConstraints(t, ctx, tx), "every legacy constraint must survive; the new i18n NOT NULL constraint is checked separately")
	require.Equal(t, indexesBefore, v16LegacyIndexes(t, ctx, tx), "legacy uniqueness, trace and monitor indexes must survive")
	require.Equal(t, historyBefore, v16MigrationHistory(t, ctx, tx))
	for _, table := range []string{"channel_monitors", "channel_monitor_request_templates"} {
		_, err = tx.ExecContext(ctx, "SAVEPOINT v16_provider_check")
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, "UPDATE "+table+" SET provider = 'cline' WHERE name = 'v16-historical-openai'")
		var checkError *pq.Error
		require.ErrorAs(t, err, &checkError)
		require.Equal(t, pq.ErrorCode("23514"), checkError.Code)
		require.Equal(t, table+"_provider_check", checkError.Constraint, "a catalog platform without a probe implementation remains rejected")
		_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT v16_provider_check")
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, "RELEASE SAVEPOINT v16_provider_check")
		require.NoError(t, err)
	}

	var blankTranslations int
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM groups WHERE i18n <> '{}'::jsonb OR i18n IS NULL").Scan(&blankTranslations))
	require.Zero(t, blankTranslations, "existing groups default to the original display text")
	var i18nNotNull bool
	var i18nDefault string
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT a.attnotnull, pg_get_expr(d.adbin, d.adrelid)
FROM pg_attribute a JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE a.attrelid = 'groups'::regclass AND a.attname = 'i18n'`).Scan(&i18nNotNull, &i18nDefault))
	require.True(t, i18nNotNull)
	require.Contains(t, i18nDefault, "'{}'::jsonb")
	const translations = `{"en":{"name":"Harbor pool"},"zh-Hant":{"description":"保留的分組設定"}}`
	_, err = tx.ExecContext(ctx, "UPDATE groups SET i18n = $2::jsonb WHERE id = $1", firstGroupID, translations)
	require.NoError(t, err)

	cache := &service.GeminiCachedContent{
		PublicID: "v16-upgrade-cache", UserID: userID, APIKeyID: keyID, GroupID: cacheGroupID, AccountID: cacheAccountID,
		UpstreamName: "cachedContents/v16-upstream", Model: "harbor-public", UpstreamModel: "models/harbor-upstream",
		DisplayName: "saved cache", TotalTokenCount: 8192, ExpireTime: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond),
		ChannelUsage: service.ChannelUsageFields{ChannelID: 73, OriginalModel: "harbor-public", ChannelMappedModel: "harbor-upstream", BillingModelSource: "requested"},
	}
	cacheRepo := &geminiCachedContentRepository{sql: tx}
	require.NoError(t, cacheRepo.Create(ctx, cache))
	acquired, err := cacheRepo.BeginMutation(ctx, cache.ID, cache.ExpireTime, "v16-lease-holder", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	cacheBefore := v16RowsSnapshot(t, ctx, tx, "gemini_cached_contents")
	for _, name := range applied {
		v16ExecuteMigration(t, ctx, tx, name)
	}
	require.Equal(t, cacheBefore, v16RowsSnapshot(t, ctx, tx, "gemini_cached_contents"), "replaying DDL preserves billing snapshots and pending leases")
	for _, table := range tables {
		require.Equal(t, snapshots[table], v16RowsSnapshot(t, ctx, tx, table), "DDL replay must preserve %s", table)
	}
	var storedTranslations string
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT i18n::text FROM groups WHERE id = $1", firstGroupID).Scan(&storedTranslations))
	require.JSONEq(t, translations, storedTranslations)
	var localized service.GroupI18n
	require.NoError(t, json.Unmarshal([]byte(storedTranslations), &localized))
	name, description := localized.Localize("en-US", "saved name", "saved description")
	require.Equal(t, "Harbor pool", name)
	require.Equal(t, "saved description", description, "missing translated fields fall back independently")
	require.Equal(t, "saved name", localized.LocalizeName("fr", "saved name"))
}

func TestV16CacheMigrationsBackfillExistingRows(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	_, err := tx.ExecContext(ctx, "DROP TABLE gemini_cached_contents")
	require.NoError(t, err)
	v16ExecuteMigration(t, ctx, tx, "243_gemini_cached_contents.sql")
	_, err = tx.ExecContext(ctx, `
INSERT INTO gemini_cached_contents (
 public_id, user_id, api_key_id, group_id, account_id, upstream_name, model, upstream_model, expire_time)
VALUES ('v16-pre-channel-usage', 1, 2, 3, 4, 'cachedContents/old', 'saved-public', 'saved-upstream', '2026-10-15T00:00:00Z')`)
	require.NoError(t, err)
	before := v16RowsSnapshot(t, ctx, tx, "gemini_cached_contents")
	v16ExecuteMigration(t, ctx, tx, "244_gemini_cached_contents_channel_usage.sql")
	v16ExecuteMigration(t, ctx, tx, "245_gemini_cached_contents_mutation_lease.sql")
	var original, usage string
	var token sql.NullString
	var lease sql.NullTime
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT (to_jsonb(c) - 'i18n' - 'channel_usage' - 'mutation_token' - 'mutation_lease_until')::text,
 channel_usage::text, mutation_token, mutation_lease_until
FROM gemini_cached_contents c WHERE public_id = 'v16-pre-channel-usage'`).Scan(&original, &usage, &token, &lease))
	require.JSONEq(t, "["+original+"]", before)
	require.JSONEq(t, `{}`, usage, "pre-244 rows receive an empty billing snapshot")
	require.False(t, token.Valid)
	require.False(t, lease.Valid)

	var notNull bool
	var defaultValue string
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT a.attnotnull, pg_get_expr(d.adbin, d.adrelid)
FROM pg_attribute a JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE a.attrelid = 'gemini_cached_contents'::regclass AND a.attname = 'channel_usage'`).Scan(&notNull, &defaultValue))
	require.True(t, notNull)
	require.Contains(t, defaultValue, "'{}'::jsonb")
	for _, index := range []struct {
		name      string
		unique    bool
		keys      string
		predicate string
	}{
		{"idx_gemini_cached_contents_public_id", true, "(public_id)", ""},
		{"idx_gemini_cached_contents_api_key_active", false, "(api_key_id, id DESC)", "(deleted_at IS NULL)"},
		{"idx_gemini_cached_contents_account", false, "(account_id)", ""},
		{"idx_gemini_cached_contents_pending_mutation", false, "(mutation_lease_until)", "((mutation_token IS NOT NULL) AND (deleted_at IS NULL))"},
	} {
		var valid, unique bool
		var definition, predicate string
		require.NoError(t, tx.QueryRowContext(ctx, `
SELECT i.indisvalid, i.indisunique, pg_get_indexdef(i.indexrelid), COALESCE(pg_get_expr(i.indpred, i.indrelid), '')
FROM pg_index i WHERE i.indexrelid = to_regclass($1)`, index.name).Scan(&valid, &unique, &definition, &predicate))
		require.True(t, valid, index.name)
		require.Equal(t, index.unique, unique, index.name)
		require.Contains(t, definition, index.keys, index.name)
		require.Equal(t, index.predicate, predicate, index.name)
	}
}

func v16ExecuteMigration(t *testing.T, ctx context.Context, tx *sql.Tx, name string) {
	t.Helper()
	content, err := dbmigrations.FS.ReadFile(name)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(content))
	require.NoError(t, err, "upgrade migration %s must accept saved Harbor rows", name)
}

func v16RowsSnapshot(t *testing.T, ctx context.Context, tx *sql.Tx, table string) string {
	t.Helper()
	var snapshot string
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT COALESCE(jsonb_agg(to_jsonb(r) - 'i18n' ORDER BY (to_jsonb(r) - 'i18n')::text), '[]'::jsonb)::text
FROM `+table+` r`).Scan(&snapshot))
	return snapshot
}

func v16MigrationHistory(t *testing.T, ctx context.Context, query geminiCachedContentSQLExecutor) string {
	t.Helper()
	var snapshot string
	require.NoError(t, query.QueryRowContext(ctx, `
SELECT jsonb_agg(to_jsonb(m) ORDER BY filename)::text FROM schema_migrations m`).Scan(&snapshot))
	var filenames []string
	rows, err := query.QueryContext(ctx, "SELECT filename FROM schema_migrations ORDER BY filename")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		filenames = append(filenames, name)
	}
	require.NoError(t, rows.Err())
	embedded, err := fs.Glob(dbmigrations.FS, "*.sql")
	require.NoError(t, err)
	require.ElementsMatch(t, embedded, filenames, "TestMain must apply every full filename, including both 242 entries")
	return snapshot
}

func v16LegacyConstraints(t *testing.T, ctx context.Context, tx *sql.Tx) string {
	t.Helper()
	var snapshot string
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT jsonb_agg(jsonb_build_object('table', r.relname, 'name', c.conname,
 'type', c.contype, 'definition', pg_get_constraintdef(c.oid), 'validated', c.convalidated)
 ORDER BY r.relname, c.conname)::text
FROM pg_constraint c JOIN pg_class r ON r.oid = c.conrelid
JOIN pg_namespace n ON n.oid = r.relnamespace
WHERE n.nspname = 'public' AND r.relname <> 'gemini_cached_contents'
 AND NOT (r.relname = 'groups' AND c.contype = 'n' AND pg_get_constraintdef(c.oid) = 'NOT NULL i18n')`).Scan(&snapshot))
	require.Contains(t, snapshot, "channel_monitors_provider_check")
	require.Contains(t, snapshot, "channel_monitor_request_templates_provider_check")
	require.NotContains(t, snapshot, "user_platform_quotas_platform_check")
	require.NotContains(t, snapshot, "composite_model_routes_target_platform_check")
	return snapshot
}

func v16LegacyIndexes(t *testing.T, ctx context.Context, tx *sql.Tx) string {
	t.Helper()
	var snapshot string
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT jsonb_agg(jsonb_build_object('table', tablename, 'name', indexname, 'definition', indexdef)
 ORDER BY tablename, indexname)::text FROM pg_indexes
WHERE schemaname = 'public' AND tablename <> 'gemini_cached_contents'`).Scan(&snapshot))
	require.Contains(t, snapshot, "userplatformquota_user_id_platform_uq")
	require.Contains(t, snapshot, "idx_composite_model_routes_unique_active")
	return snapshot
}
