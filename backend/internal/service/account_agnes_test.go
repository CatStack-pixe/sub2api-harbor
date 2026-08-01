package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgnesAccountOpenAICompatibility(t *testing.T) {
	account := &Account{
		Platform: PlatformAgnes,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "agnes-secret",
		},
	}

	require.True(t, account.IsAgnes())
	require.True(t, account.IsOpenAICompatible())
	require.Equal(t, AgnesDefaultBaseURL, account.GetOpenAIBaseURL())
	require.Equal(t, "agnes-secret", account.GetOpenAIApiKey())
	require.False(t, account.ShouldUseOpenAIResponsesAPI())
	require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
	require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
}

func TestAgnesAccountHonorsCustomBaseURL(t *testing.T) {
	account := &Account{
		Platform: PlatformAgnes,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "agnes-secret",
			"base_url": "https://agnes-relay.example/v1",
		},
	}

	require.Equal(t, "https://agnes-relay.example/v1", account.GetOpenAIBaseURL())
}

func TestNormalizeOpenAICompatiblePlatformPreservesAgnes(t *testing.T) {
	require.Equal(t, PlatformAgnes, normalizeOpenAICompatiblePlatform(PlatformAgnes))
}
