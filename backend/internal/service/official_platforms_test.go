package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOfficialPlatformCredentialsAndBaseURLs(t *testing.T) {
	tests := []struct {
		platform string
		baseURL  string
	}{
		{PlatformModelScope, ModelScopeDefaultBaseURL},
		{PlatformDashScope, DashScopeDefaultBaseURL},
		{PlatformMiniMax, MiniMaxDefaultBaseURL},
		{PlatformVolcengine, VolcengineDefaultBaseURL},
	}

	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			require.NoError(t, ValidateAccountPlatform(tt.platform))
			require.NoError(t, validateAccountCredentials(tt.platform, AccountTypeAPIKey, map[string]any{"api_key": "test-key"}))
			require.Error(t, validateAccountCredentials(tt.platform, AccountTypeOAuth, map[string]any{"api_key": "test-key"}))
			account := &Account{Platform: tt.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
			require.True(t, account.IsOpenAICompatible())
			require.Equal(t, tt.baseURL, account.GetOpenAIBaseURL())
			require.False(t, account.ShouldUseOpenAIResponsesAPI())
		})
	}
}

func TestOfficialPlatformModelsRequestURLs(t *testing.T) {
	service := &AccountTestService{cfg: &config.Config{}}
	tests := []struct {
		platform string
		baseURL  string
		wantURL  string
	}{
		{PlatformModelScope, ModelScopeDefaultBaseURL, ModelScopeDefaultBaseURL + "/models"},
		{PlatformDashScope, DashScopeDefaultBaseURL, DashScopeDefaultBaseURL + "/models"},
		{PlatformMiniMax, MiniMaxDefaultBaseURL, MiniMaxDefaultBaseURL + "/models"},
		{PlatformVolcengine, VolcengineDefaultBaseURL, VolcengineDefaultBaseURL + "/models"},
	}

	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			account := &Account{Platform: tt.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"api_key":  "test-key",
				"base_url": tt.baseURL,
			}}
			req, err := service.buildUpstreamModelsRequest(context.Background(), account)
			require.NoError(t, err)
			require.Equal(t, tt.wantURL, req.URL.String())
			require.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
		})
	}
}
