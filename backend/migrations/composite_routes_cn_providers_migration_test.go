package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompositeRoutesCNProvidersMigration(t *testing.T) {
	content, err := FS.ReadFile("227_composite_routes_add_cn_providers.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check")
	for _, platform := range []string{"anthropic", "openai", "gemini", "antigravity", "grok", "agnes", "deepseek", "nvidia", "tokenrhythm", "kimi", "zhipu", "chatanywhere", "glm"} {
		require.Contains(t, sql, "'"+platform+"'")
	}
}

func TestFinalPlatformConstraintUnionMigration(t *testing.T) {
	content, err := FS.ReadFile("230_add_official_platform_constraints.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "user_platform_quotas_platform_check")
	require.Contains(t, sql, "composite_model_routes_target_platform_check")
	for _, platform := range []string{"anthropic", "openai", "gemini", "antigravity", "grok", "agnes", "deepseek", "nvidia", "tokenrhythm", "kimi", "zhipu", "chatanywhere", "glm", "modelscope", "dashscope", "minimax", "volcengine"} {
		require.Contains(t, sql, "'"+platform+"'")
	}
}
