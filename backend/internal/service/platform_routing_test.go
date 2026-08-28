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
