package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxPlatformMigration(t *testing.T) {
	content, err := FS.ReadFile("237_add_minimax_platform.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql,
		"CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'agnes', 'deepseek', 'nvidia', 'tokenrhythm', 'kimi', 'zhipu', 'chatanywhere', 'glm', 'modelscope', 'dashscope', 'minimax', 'volcengine', 'sensenova'))")
	require.Contains(t, sql,
		"CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'agnes', 'deepseek', 'nvidia', 'tokenrhythm', 'kimi', 'zhipu', 'chatanywhere', 'glm', 'modelscope', 'dashscope', 'minimax', 'volcengine', 'sensenova'))")
	require.Contains(t, sql,
		"CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'antigravity', 'kimi', 'zhipu', 'deepseek', 'sensenova', 'minimax'))")
}
