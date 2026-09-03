package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAICompatibleGroupsAllowCompatibleAccountPlatforms(t *testing.T) {
	accountPlatforms := schedulerSnapshotPlatforms()
	groupPlatforms := append([]string{PlatformComposite}, openAICompatibleGroupPlatforms()...)
	for _, groupPlatform := range groupPlatforms {
		for _, accountPlatform := range accountPlatforms {
			expected := (&Account{Platform: accountPlatform}).IsOpenAICompatible()
			if groupPlatform == PlatformComposite {
				expected = true
			}
			require.Equal(t, expected, accountPlatformMatchesGroup(groupPlatform, accountPlatform),
				"group %q should accept account platform %q", groupPlatform, accountPlatform)
		}
	}
}

func TestNativeGroupsKeepProtocolIsolation(t *testing.T) {
	for _, groupPlatform := range []string{PlatformAnthropic, PlatformGemini, PlatformAntigravity} {
		for _, accountPlatform := range schedulerSnapshotPlatforms() {
			require.Equal(t, accountPlatform == groupPlatform,
				accountPlatformMatchesGroup(groupPlatform, accountPlatform),
				"native group %q must keep account platform %q isolated", groupPlatform, accountPlatform)
		}
	}
}

func TestAccountPlatformQueriesUseCompatiblePools(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformGemini, PlatformAntigravity} {
		require.Equal(t, []string{platform}, accountPlatformsForGroupPlatform(platform))
	}
	require.ElementsMatch(t, openAICompatibleGroupPlatforms(), accountPlatformsForGroupPlatform(PlatformZhipu))
	allPlatforms := schedulerSnapshotPlatforms()
	require.ElementsMatch(t, allPlatforms[:], accountPlatformsForGroupPlatform(PlatformComposite))
}

func TestSchedulerPlatformsForAccountProjectsCrossPlatformMembership(t *testing.T) {
	compatible := (&Account{Platform: PlatformDeepSeek}).IsOpenAICompatible()
	projected := schedulerPlatformsForAccount(&Account{Platform: PlatformDeepSeek})
	for _, platform := range projected {
		require.True(t, (&Account{Platform: platform}).IsOpenAICompatible(),
			"deepseek account should only project into compatible request buckets")
	}
	if compatible {
		require.Contains(t, projected, PlatformOpenAI)
		require.Contains(t, projected, PlatformDeepSeek)
		require.NotContains(t, projected, PlatformAnthropic)
	}

	antigravity := &Account{
		Platform: PlatformAntigravity,
		Extra:    map[string]any{"mixed_scheduling": true},
	}
	require.ElementsMatch(t,
		[]string{PlatformAntigravity, PlatformAnthropic, PlatformGemini},
		schedulerPlatformsForAccount(antigravity),
	)
}

func TestCopyingAccountsBetweenAnyValidGroupPlatformsIsAllowed(t *testing.T) {
	platforms := schedulerSnapshotPlatforms()
	groupPlatforms := append([]string{PlatformComposite}, platforms[:]...)
	for _, target := range groupPlatforms {
		for _, source := range groupPlatforms {
			require.True(t, canCopyAccountsFromGroupPlatform(target, source),
				"valid group platform pair %q <- %q should be copyable", target, source)
		}
	}
	require.False(t, canCopyAccountsFromGroupPlatform("unknown", PlatformOpenAI))
	require.False(t, canCopyAccountsFromGroupPlatform(PlatformOpenAI, "unknown"))
}

func TestInvalidPlatformsAreNotCompatible(t *testing.T) {
	require.False(t, accountPlatformMatchesGroup("unknown", PlatformOpenAI))
	require.False(t, accountPlatformMatchesGroup(PlatformOpenAI, "unknown"))
	require.False(t, accountPlatformMatchesGroup(PlatformOpenAI, PlatformComposite))
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

func TestZhipuGroupModelOptionsIncludeTokenRhythmMappings(t *testing.T) {
	group := &Group{Platform: PlatformZhipu}
	accounts := []Account{{
		Platform: PlatformTokenRhythm,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"glm-5.3": "glm-5.3",
			},
		},
	}}

	require.Equal(t, []string{"glm-5.3"}, groupModelOptionsFromAccounts(group, accounts))
}
