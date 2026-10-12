package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// These explicit fixtures retain all Harbor providers and their historical
// precedence while adding Command Code and Cline.
var (
	legacyDisplayOrder = []string{
		PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformGrok,
		PlatformAgnes, PlatformDeepSeek, PlatformNvidia, PlatformTokenRhythm, PlatformTierflow,
		PlatformSenseAudio, PlatformKimi, PlatformZhipu, PlatformChatAnywhere, PlatformGLM,
		PlatformModelScope, PlatformDashScope, PlatformMiniMax, PlatformVolcengine, PlatformSenseNova,
		PlatformOpenCodeGo, PlatformTypeSafe, PlatformCommandCode, PlatformCline,
	}
	legacyCompositePrecedence = []string{
		PlatformAnthropic, PlatformGemini, PlatformOpenAI, PlatformAntigravity, PlatformGrok,
		PlatformAgnes, PlatformDeepSeek, PlatformNvidia, PlatformTokenRhythm, PlatformKimi,
		PlatformZhipu, PlatformChatAnywhere, PlatformGLM, PlatformModelScope, PlatformDashScope,
		PlatformMiniMax, PlatformVolcengine, PlatformSenseNova, PlatformSenseAudio, PlatformTierflow,
		PlatformOpenCodeGo, PlatformTypeSafe, PlatformCommandCode, PlatformCline,
	}
	legacyOpenAIGateway = []string{
		PlatformOpenAI, PlatformGrok, PlatformAgnes, PlatformDeepSeek, PlatformNvidia, PlatformTokenRhythm,
		PlatformTierflow, PlatformSenseAudio, PlatformKimi, PlatformZhipu, PlatformChatAnywhere, PlatformGLM,
		PlatformModelScope, PlatformDashScope, PlatformMiniMax, PlatformVolcengine, PlatformSenseNova,
		PlatformOpenCodeGo, PlatformCommandCode, PlatformCline,
	}
	legacyCNProviders    = []string{PlatformDeepSeek, PlatformSenseAudio, PlatformKimi, PlatformZhipu, PlatformMiniMax, PlatformSenseNova}
	legacyLiteLLMByPlatf = map[string]string{
		PlatformAnthropic: "anthropic", PlatformOpenAI: "openai", PlatformGemini: "gemini",
		PlatformAntigravity: "anthropic", PlatformGrok: "xai", PlatformKimi: "moonshot",
		PlatformZhipu: "zhipu", PlatformDeepseek: "deepseek", PlatformMiniMax: "minimax",
		PlatformOpenCodeGo: "opencode-go", PlatformTypeSafe: "typesafe", PlatformCommandCode: "", PlatformCline: "",
	}
)

func TestPlatformListMatchesLegacyLists(t *testing.T) {
	require.Equal(t, legacyDisplayOrder, ConcretePlatformIDs())
	require.Equal(t, legacyCompositePrecedence, CompositePrecedencePlatformIDs())
	require.Equal(t, legacyOpenAIGateway, PlatformIDsWhere(func(spec PlatformSpec) bool {
		return spec.Gateway == PlatformGatewayOpenAI
	}))
	require.Equal(t, legacyCNProviders, PlatformIDsWhere(func(spec PlatformSpec) bool { return spec.CNProvider }))
	require.Len(t, ConcretePlatformIDs(), 24)
	for _, platform := range legacyDisplayOrder {
		spec, ok := LookupPlatform(platform)
		require.True(t, ok, platform)
		require.Equal(t, legacyLiteLLMByPlatf[platform], spec.LiteLLMProvider, platform)
		require.Equal(t, UsesOpenAIGateway(platform), spec.Gateway == PlatformGatewayOpenAI, platform)
		require.Equal(t, IsCNProviderPlatform(platform), spec.CNProvider, platform)
	}
}

func TestPlatformListPredicates(t *testing.T) {
	for _, platform := range legacyDisplayOrder {
		require.True(t, IsConcretePlatform(platform), platform)
		require.True(t, IsGroupPlatform(platform), platform)
		spec, ok := LookupPlatform(platform)
		require.True(t, ok, platform)
		require.NotEmpty(t, spec.DisplayName, platform)
		require.NotEmpty(t, spec.Gateway, platform)
	}
	require.False(t, IsConcretePlatform(PlatformComposite))
	require.True(t, IsGroupPlatform(PlatformComposite))
	for _, invalid := range []string{"", "moonshot", "Kimi", "openai ", "kiro", "bogus"} {
		require.False(t, IsConcretePlatform(invalid), invalid)
		require.False(t, IsGroupPlatform(invalid), invalid)
		require.False(t, UsesOpenAIGateway(invalid), invalid)
		require.False(t, IsCNProviderPlatform(invalid), invalid)
		_, ok := LookupPlatform(invalid)
		require.False(t, ok, invalid)
	}
}

func TestPlatformsReturnsCopy(t *testing.T) {
	platforms := Platforms()
	platforms[0].ID = "mutated"
	require.Equal(t, PlatformAnthropic, Platforms()[0].ID)
	ids := ConcretePlatformIDs()
	ids[0] = "mutated"
	require.Equal(t, PlatformAnthropic, ConcretePlatformIDs()[0])
	precedence := CompositePrecedencePlatformIDs()
	precedence[0] = "mutated"
	require.Equal(t, legacyCompositePrecedence, CompositePrecedencePlatformIDs())
}

func TestPlatformListHasUniqueIDsAndRetainsDeepSeekAlias(t *testing.T) {
	require.Equal(t, PlatformDeepSeek, PlatformDeepseek)
	require.Equal(t, len(ConcretePlatformIDs()), len(platformIndex))
	require.Len(t, PlatformIDsWhere(func(spec PlatformSpec) bool {
		return spec.ID == PlatformDeepseek
	}), 1)
}
