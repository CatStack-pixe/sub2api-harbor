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
	for _, platform := range []string{
		"anthropic", "openai", "gemini", "antigravity", "grok", "agnes",
		"deepseek", "nvidia", "tokenrhythm", "kimi", "zhipu", "chatanywhere", "glm",
	} {
		require.Contains(t, sql, "'"+platform+"'")
	}
}
