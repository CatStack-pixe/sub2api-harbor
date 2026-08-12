//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateDeepSeekAccountCredentials(t *testing.T) {
	tests := []struct {
		name        string
		accountType string
		credentials map[string]any
		wantErr     string
	}{
		{
			name:        "api key with default base url",
			accountType: AccountTypeAPIKey,
			credentials: map[string]any{"api_key": "sk-deepseek"},
		},
		{
			name:        "api key with custom base url",
			accountType: AccountTypeAPIKey,
			credentials: map[string]any{"api_key": "sk-deepseek", "base_url": "https://relay.example/v1"},
		},
		{
			name:        "missing api key",
			accountType: AccountTypeAPIKey,
			credentials: map[string]any{},
			wantErr:     "DEEPSEEK_API_KEY_REQUIRED",
		},
		{
			name:        "wrong account type",
			accountType: AccountTypeOAuth,
			credentials: map[string]any{"api_key": "sk-deepseek"},
			wantErr:     "DEEPSEEK_ACCOUNT_TYPE_UNSUPPORTED",
		},
		{
			name:        "relative base url",
			accountType: AccountTypeAPIKey,
			credentials: map[string]any{"api_key": "sk-deepseek", "base_url": "/v1"},
			wantErr:     "DEEPSEEK_BASE_URL_INVALID",
		},
		{
			name:        "unsupported base url scheme",
			accountType: AccountTypeAPIKey,
			credentials: map[string]any{"api_key": "sk-deepseek", "base_url": "ftp://relay.example/v1"},
			wantErr:     "DEEPSEEK_BASE_URL_INVALID",
		},
		{
			name:        "non-string base url",
			accountType: AccountTypeAPIKey,
			credentials: map[string]any{"api_key": "sk-deepseek", "base_url": 123},
			wantErr:     "DEEPSEEK_BASE_URL_INVALID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAccountCredentials(PlatformDeepSeek, tt.accountType, tt.credentials)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestDedicatedAccountGroupPlatformIsolation(t *testing.T) {
	require.True(t, accountCanBindToGroupPlatform(PlatformDeepSeek, PlatformDeepSeek))
	require.True(t, accountCanBindToGroupPlatform(PlatformDeepSeek, PlatformComposite))
	require.False(t, accountCanBindToGroupPlatform(PlatformDeepSeek, PlatformOpenAI))
	require.False(t, accountCanBindToGroupPlatform(PlatformOpenAI, PlatformDeepSeek))
	require.True(t, accountCanBindToGroupPlatform(PlatformNvidia, PlatformNvidia))
	require.True(t, accountCanBindToGroupPlatform(PlatformNvidia, PlatformComposite))
	require.False(t, accountCanBindToGroupPlatform(PlatformNvidia, PlatformOpenAI))
	require.False(t, accountCanBindToGroupPlatform(PlatformOpenAI, PlatformNvidia))
	require.True(t, accountCanBindToGroupPlatform(PlatformOpenAI, PlatformOpenAI))
}

func TestDeepSeekAccountCompatibilityCapabilities(t *testing.T) {
	account := &Account{
		Platform: PlatformDeepSeek,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-deepseek",
		},
	}

	require.True(t, account.IsDeepSeek())
	require.True(t, account.IsOpenAICompatible())
	require.Equal(t, DeepSeekDefaultBaseURL, account.GetOpenAIBaseURL())
	require.Equal(t, "sk-deepseek", account.GetOpenAIApiKey())
	require.False(t, account.ShouldUseOpenAIResponsesAPI())
	require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
	require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
}
