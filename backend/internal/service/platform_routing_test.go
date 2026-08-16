package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeepSeekGroupAllowsTokenRhythmAccounts(t *testing.T) {
	require.True(t, accountPlatformMatchesGroup(PlatformDeepSeek, PlatformDeepSeek))
	require.True(t, accountPlatformMatchesGroup(PlatformDeepSeek, PlatformTokenRhythm))
	require.False(t, accountPlatformMatchesGroup(PlatformTokenRhythm, PlatformDeepSeek))
}

func TestDeepSeekGroupModelOptionsIncludeTokenRhythmMappings(t *testing.T) {
	group := &Group{Platform: PlatformDeepSeek}
	accounts := []Account{{
		Platform: PlatformTokenRhythm,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"deepseek-v4-flash": "deepseek-v4-flash",
			},
		},
	}}

	require.Equal(t, []string{"deepseek-v4-flash"}, groupModelOptionsFromAccounts(group, accounts))
}
