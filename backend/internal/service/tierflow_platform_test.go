package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestTierflowAccountPlatformContract(t *testing.T) {
	account := &Account{
		Platform: PlatformTierflow,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":    "test-tierflow-key",
			"user_agent": "test-agent",
		},
	}
	require.NoError(t, ValidateAccountPlatform(PlatformTierflow))
	require.NoError(t, ValidateGroupPlatform(PlatformTierflow))
	require.True(t, IsAllowedQuotaPlatform(PlatformTierflow))
	require.True(t, isConcreteRequestPlatform(PlatformTierflow))
	require.True(t, account.IsTierflow())
	require.True(t, account.IsOpenAICompatible())
	require.False(t, account.IsCNProvider(), "a relay must not inherit native CN provider model normalization")
	require.False(t, account.IsMultiProtocolAPIKey())
	require.Equal(t, "https://tierflow.cn/v1", account.GetOpenAIBaseURL())
	require.Equal(t, "test-tierflow-key", account.GetOpenAIProtocolAPIKey())
	require.Equal(t, "test-agent", account.GetOpenAIUserAgent())
	require.True(t, account.IsHeaderOverrideEligible())
	require.False(t, account.ShouldUseOpenAIResponsesAPI())
	require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
	require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
	require.Equal(t, PlatformTierflow, NormalizeOpenAICompatiblePlatform(PlatformTierflow))
	require.Contains(t, accountPlatformsForGroupPlatform(PlatformOpenAI), PlatformTierflow)
	require.Contains(t, accountPlatformsForGroupPlatform(PlatformTierflow), PlatformDeepSeek)
	require.Contains(t, schedulerPlatformsForAccount(account), PlatformTierflow)
	require.Contains(t, schedulerPlatformsForAccount(account), PlatformOpenAI)
	require.NotContains(t, schedulerPlatformsForAccount(account), PlatformAnthropic)
	require.True(t, profitControlPlatformSupported(PlatformTierflow))
	require.Empty(t, defaultModelsListCandidateIDs(PlatformTierflow))
	for _, model := range []string{"custom/unknown-model", "deepseek-v4-flash", "claude-custom-model", "gpt-special-model"} {
		require.True(t, account.IsModelSupported(model), model)
		require.Equal(t, model, account.GetMappedModel(model))
		require.Equal(t, model, normalizeOpenAIModelForUpstream(account, model))
	}
	account.Credentials["base_url"] = "https://relay.example/custom/v1/"
	require.Equal(t, "https://relay.example/custom/v1", account.GetOpenAIBaseURL())
}

func TestTierflowCredentials(t *testing.T) {
	for _, baseURL := range []string{"", TierflowDefaultBaseURL, "https://relay.example/custom/v1"} {
		require.NoError(t, validateAccountCredentials(PlatformTierflow, AccountTypeAPIKey,
			map[string]any{"api_key": "test-key", "base_url": baseURL}))
	}
	for _, credentials := range []map[string]any{
		nil,
		{"api_key": " "},
		{"api_key": "test-key", "base_url": 42},
		{"api_key": "test-key", "base_url": "relative/path"},
		{"api_key": "test-key", "base_url": "ftp://relay.example/v1"},
	} {
		require.Error(t, validateAccountCredentials(PlatformTierflow, AccountTypeAPIKey, credentials))
	}
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeUpstream} {
		require.Error(t, validateAccountCredentials(PlatformTierflow, accountType, map[string]any{"api_key": "test-key"}))
	}
}

func TestTierflowModelsRequestUsesAPIKeyOnly(t *testing.T) {
	svc := &AccountTestService{cfg: &config.Config{}}
	account := &Account{Platform: PlatformTierflow, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "test-key", "session_cookie": "test-private-session", "password": "test-private-password",
	}}
	req, err := svc.buildUpstreamModelsRequest(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "https://tierflow.cn/v1/models", req.URL.String())
	require.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
	require.Empty(t, req.Header.Get("Cookie"))
	require.Empty(t, req.URL.RawQuery)
}

func TestTierflowProbeRequiresAnActualCatalogModel(t *testing.T) {
	require.Empty(t, tierflowAccountTestModel(nil, ""))
	account := &Account{Platform: PlatformTierflow, Type: AccountTypeAPIKey}
	require.Empty(t, tierflowAccountTestModel(account, ""))
	require.Equal(t, "custom/model", tierflowAccountTestModel(account, " custom/model "))
	account.Credentials = map[string]any{"model_mapping": map[string]any{
		"z-model": "upstream/z", "a-model": "upstream/a", "*": "*", "empty": "",
	}}
	require.Equal(t, "a-model", tierflowAccountTestModel(account, ""))
	require.Equal(t, "upstream/a", account.GetMappedModel(tierflowAccountTestModel(account, "")))
}

func TestTierflowMonitorAndCompositeRegistration(t *testing.T) {
	require.NoError(t, validateProvider(MonitorProviderTierflow))
	require.True(t, providerSupportsProbe(MonitorProviderTierflow))
	require.True(t, isOpenAICompatibleChatProvider(MonitorProviderTierflow))
	require.NoError(t, validateAPIMode(MonitorProviderTierflow, MonitorAPIModeChatCompletions))
	require.Error(t, validateAPIMode(MonitorProviderTierflow, MonitorAPIModeResponses))
	require.Error(t, validateReplaceRequestBody(MonitorProviderTierflow, MonitorAPIModeChatCompletions, map[string]any{}))
	adapter, ok := providerAdapters[MonitorProviderTierflow]
	require.True(t, ok)
	require.Equal(t, "/v1/chat/completions", adapter.buildPath("arbitrary-model"))
	require.Equal(t, "Bearer test-key", adapter.buildHeaders("test-key")["Authorization"])
	for _, model := range []string{"tierflow/custom-model", "tierflow-custom-model", "tierflow", "tierflow_pro"} {
		platform, ok := DetectModelPlatform(model)
		require.True(t, ok)
		require.Equal(t, PlatformTierflow, platform)
	}
}
