package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeepSeekGroupAllowsTokenRhythmAccounts(t *testing.T) {
	require.True(t, accountPlatformMatchesGroup(PlatformDeepSeek, PlatformDeepSeek))
	require.True(t, accountPlatformMatchesGroup(PlatformDeepSeek, PlatformTokenRhythm))
	require.False(t, accountPlatformMatchesGroup(PlatformTokenRhythm, PlatformDeepSeek))
}

func TestOpenAIGroupAllowsChatAnywhereAccounts(t *testing.T) {
	require.True(t, accountPlatformMatchesGroup(PlatformOpenAI, PlatformOpenAI))
	require.True(t, accountPlatformMatchesGroup(PlatformOpenAI, PlatformChatAnywhere))
	require.False(t, accountPlatformMatchesGroup(PlatformChatAnywhere, PlatformOpenAI))
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

func TestExplicitOpenAICompatibleGroupAllowsEveryCompatibleProvider(t *testing.T) {
	require.True(t, accountPlatformMatchesExplicitGroup(PlatformDeepSeek, PlatformNvidia))
	require.True(t, accountPlatformMatchesExplicitGroup(PlatformNvidia, PlatformDeepSeek))
	require.False(t, accountPlatformMatchesExplicitGroup(PlatformDeepSeek, PlatformAnthropic))
}

func TestSchedulingScopeUsesGroupMembershipForCompatibleProviders(t *testing.T) {
	groupID := int64(18)
	account := &Account{
		Platform:    PlatformNvidia,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}

	require.True(t, accountPlatformMatchesSchedulingScope(&groupID, PlatformDeepSeek, account.Platform))
	require.False(t, accountPlatformMatchesSchedulingScope(nil, PlatformDeepSeek, account.Platform))
	require.False(t, accountPlatformMatchesSchedulingScope(&groupID, PlatformAnthropic, account.Platform))
	require.True(t, isOpenAICompatibleAccountEligibleForRequestInGroup(
		context.Background(), account, &groupID, PlatformDeepSeek, "", false, "",
	))
}
