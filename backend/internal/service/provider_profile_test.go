//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// legacyProviderDefaultBaseURL 复述 provider profile 引入前 account.go 中逐平台
// switch 的默认端点逻辑，作为等价性基准。
func legacyProviderDefaultBaseURL(platform, mode, protocol string) string {
	coding := mode == AccountModeCoding
	zen := mode == AccountModeZen
	switch protocol {
	case APIProtocolAnthropic:
		switch platform {
		case PlatformKimi:
			if coding {
				return DefaultKimiCodingAnthropicBaseURL
			}
			return DefaultKimiPayGAnthropicBaseURL
		case PlatformZhipu:
			return DefaultZhipuAnthropicBaseURL
		case PlatformDeepseek:
			return DefaultDeepseekAnthropicBaseURL
		case PlatformSenseAudio:
			return SenseAudioDefaultBaseURL
		case PlatformMiniMax:
			return DefaultMiniMaxAnthropicBaseURL
		case PlatformOpenCodeGo:
			if zen {
				return DefaultOpenCodeZenAnthropicBaseURL
			}
			return DefaultOpenCodeGoAnthropicBaseURL
		}
	case APIProtocolChatCompletions, APIProtocolResponses:
		if (platform == PlatformZhipu || platform == PlatformSenseNova) && protocol == APIProtocolResponses {
			// The old switch also returned the chat base for unsupported
			// Responses protocols. Profiles omit those native endpoints;
			// routing guards continue to use the chat compatibility bridge.
			return ""
		}
		switch platform {
		case PlatformKimi:
			if coding {
				return DefaultKimiCodingBaseURL
			}
			return DefaultKimiPayGBaseURL
		case PlatformZhipu:
			if coding {
				return DefaultZhipuCodingBaseURL
			}
			return DefaultZhipuPayGBaseURL
		case PlatformDeepseek:
			return DefaultDeepseekBaseURL
		case PlatformSenseNova:
			return SenseNovaDefaultBaseURL
		case PlatformSenseAudio:
			return SenseAudioDefaultBaseURL
		case PlatformMiniMax:
			return DefaultMiniMaxBaseURL
		case PlatformOpenCodeGo:
			if zen {
				return DefaultOpenCodeZenBaseURL
			}
			return DefaultOpenCodeGoBaseURL
		}
	}
	return ""
}

func TestProviderProfile_DefaultBaseURLsMatchLegacySwitches(t *testing.T) {
	t.Parallel()

	platforms := []string{PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformSenseNova, PlatformSenseAudio, PlatformOpenCodeGo}
	modes := []string{"", AccountModePayG, AccountModeCoding, AccountModeZen, AccountModeGo, " coding ", "unknown"}

	for _, platform := range platforms {
		for _, mode := range modes {
			legacyMode := mode
			if legacyMode == " coding " {
				legacyMode = AccountModeCoding
			}
			newAccount := func(protocol string) *Account {
				return &Account{
					Platform: platform,
					Type:     AccountTypeAPIKey,
					Credentials: map[string]any{
						"api_key":      "sk-test",
						"account_mode": mode,
						"api_protocol": protocol,
					},
				}
			}
			name := platform + "/" + mode

			chat := newAccount(APIProtocolChatCompletions)
			require.Equal(t, legacyProviderDefaultBaseURL(platform, legacyMode, APIProtocolChatCompletions), chat.GetOpenAIBaseURL(), name)

			anthropic := newAccount(APIProtocolAnthropic)
			require.Equal(t, legacyProviderDefaultBaseURL(platform, legacyMode, APIProtocolAnthropic), anthropic.GetAnthropicProtocolBaseURL(), name)
			require.Equal(t, legacyProviderDefaultBaseURL(platform, legacyMode, APIProtocolChatCompletions), anthropic.GetOpenAIFormatBaseURL(), name)

			adaptive := newAccount(APIProtocolAdaptive)
			for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolResponses, APIProtocolAnthropic, "unknown"} {
				require.Equal(t, legacyProviderDefaultBaseURL(platform, legacyMode, protocol), adaptive.GetCNProtocolBaseURL(protocol), name+"/"+protocol)
			}
		}
	}
}

func TestProviderProfile_ProfilesMatchMultiProtocolPlatforms(t *testing.T) {
	t.Parallel()

	for _, platform := range platformProbeValues {
		want := legacyIsCNProvider(platform) || platform == PlatformOpenCodeGo || platform == PlatformCommandCode || platform == PlatformCline
		require.Equal(t, want, IsMultiProtocolAPIKeyProvider(platform), platform)
		require.Equal(t, want, LookupProviderProfile(platform) != nil, platform)
	}

	for platform, profile := range providerProfiles {
		require.Equal(t, platform, profile.Platform)
		_, ok := profile.Modes[profile.DefaultMode]
		require.True(t, ok, "%s default mode %q must be registered", platform, profile.DefaultMode)
	}
}

func TestProviderProfile_NativeResponsesMatchesLegacy(t *testing.T) {
	t.Parallel()

	legacy := map[string]bool{
		PlatformDeepseek:    true,
		PlatformKimi:        true,
		PlatformMiniMax:     true,
		PlatformSenseAudio:  true,
		PlatformOpenCodeGo:  true,
		PlatformCommandCode: true,
	}
	modes := []string{"", AccountModePayG, AccountModeCoding, AccountModeZen, AccountModeGo, "unknown"}
	for _, platform := range legacyAllPlatforms {
		for _, mode := range modes {
			account := &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": mode}}
			require.Equal(t, legacy[platform], account.SupportsNativeCNResponses(), platform+"/"+mode)
		}
	}
	require.False(t, (*Account)(nil).SupportsNativeCNResponses())
}

func TestProviderProfile_BaseURLKeysAreNativeProtocols(t *testing.T) {
	t.Parallel()

	for platform, profile := range providerProfiles {
		for mode, endpoints := range profile.Modes {
			require.NotEmpty(t, endpoints.BaseURLs[APIProtocolChatCompletions], "%s/%s must have a chat_completions base", platform, mode)
			for protocol, baseURL := range endpoints.BaseURLs {
				require.True(t, isNativeUpstreamProtocol(protocol), "%s/%s has unknown protocol key %q", platform, mode, protocol)
				require.NotEmpty(t, baseURL, "%s/%s/%s", platform, mode, protocol)
			}
		}
	}
}

func TestProviderProfile_ResponsesPathMatchesLegacy(t *testing.T) {
	t.Parallel()

	legacy := func(platform, base string) string {
		if platform == PlatformDeepseek {
			return buildOpenAIEndpointURL(base, "/responses")
		}
		return buildOpenAIResponsesURL(base)
	}
	bases := []string{
		"https://api.deepseek.com", "https://relay.example.com", "https://relay.example.com/v1",
		"https://opencode.ai/zen/go/v1", "https://open.bigmodel.cn/api/paas/v4", "https://api.moonshot.cn/v1",
	}
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo} {
		for _, base := range bases {
			require.Equal(t, legacy(platform, base), buildOpenAIResponsesURLForPlatform(platform, base), platform+" "+base)
		}
	}
}

func TestProviderProfile_OpenCodeProtocolRulesByMode(t *testing.T) {
	t.Parallel()

	openCode := LookupProviderProfile(PlatformOpenCodeGo)
	require.Equal(t, DefaultOpenCodeGoProtocolRules(), openCode.Endpoints(AccountModeGo).ProtocolRules)
	require.Equal(t, DefaultOpenCodeGoProtocolRules(), openCode.Endpoints("").ProtocolRules)
	require.Equal(t, DefaultOpenCodeZenProtocolRules(), openCode.Endpoints(AccountModeZen).ProtocolRules)
	require.Empty(t, LookupProviderProfile(PlatformKimi).Endpoints(AccountModeCoding).ProtocolRules)
}

func TestProviderProfile_SenseNovaDefaultsAndCustomAnthropic(t *testing.T) {
	t.Parallel()

	profile := LookupProviderProfile(PlatformSenseNova)
	require.NotNil(t, profile)
	require.Equal(t, ProviderRoutingByInbound, profile.Routing)
	require.Equal(t, map[string]string{
		APIProtocolChatCompletions: SenseNovaDefaultBaseURL,
	}, profile.Endpoints(AccountModePayG).BaseURLs)
	require.False(t, profile.SupportsProtocol("", APIProtocolResponses))
	require.False(t, profile.SupportsProtocol("", APIProtocolAnthropic))

	account := &Account{
		Platform: PlatformSenseNova,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-test",
		},
	}
	require.True(t, account.IsCNProvider())
	require.True(t, account.IsMultiProtocolAPIKey())
	require.Equal(t, SenseNovaDefaultBaseURL, account.GetCNProtocolBaseURL(APIProtocolChatCompletions))
	require.False(t, account.ShouldUseOpenAIResponsesAPI())

	account.Credentials["api_protocol"] = APIProtocolAnthropic
	account.Credentials["base_url"] = "https://sensenova-relay.example.com/anthropic"
	require.Equal(t, APIProtocolAnthropic, account.GetAPIProtocol())
	require.Equal(t, "https://sensenova-relay.example.com/anthropic", account.GetAnthropicProtocolBaseURL())
	require.Equal(t, SenseNovaDefaultBaseURL, account.GetOpenAIFormatBaseURL())
}

func TestProviderProfile_SenseAudioRetainsAllNativeProtocols(t *testing.T) {
	t.Parallel()

	profile := LookupProviderProfile(PlatformSenseAudio)
	require.NotNil(t, profile)
	require.Equal(t, ProviderRoutingByInbound, profile.Routing)
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolResponses, APIProtocolAnthropic} {
		require.Equal(t, SenseAudioDefaultBaseURL, profile.DefaultBaseURL("", protocol), protocol)
		require.True(t, profile.SupportsProtocol("", protocol), protocol)
	}
	require.Equal(t, "https://api.senseaudio.cn/v1/responses",
		buildOpenAIResponsesURLForPlatform(PlatformSenseAudio, profile.DefaultBaseURL("", APIProtocolResponses)))
	require.Equal(t, "https://api.senseaudio.cn/v1/messages",
		buildOpenAIEndpointURL(profile.DefaultBaseURL("", APIProtocolAnthropic), "/v1/messages"))
}

func TestProviderProfile_KimiAdaptiveResponsesPreservesConfiguredOptIn(t *testing.T) {
	t.Parallel()

	account := &Account{
		Platform: PlatformKimi,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":      "sk-test",
			"api_protocol": APIProtocolAdaptive,
		},
	}
	require.True(t, account.SupportsNativeCNResponses())
	require.False(t, account.UsesNativeCNResponses())
	account.Credentials["api_base_urls"] = map[string]any{
		APIProtocolResponses: " https://kimi-relay.example.com/v1 ",
	}
	require.True(t, account.UsesNativeCNResponses())
	require.Equal(t, "https://kimi-relay.example.com/v1", account.GetCNProtocolBaseURL(APIProtocolResponses))
}
