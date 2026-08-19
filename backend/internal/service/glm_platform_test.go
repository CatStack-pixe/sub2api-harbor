package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGLMPlatformCredentialsAndDefaults(t *testing.T) {
	credentials := map[string]any{"api_key": "glm-key"}
	require.NoError(t, validateAccountCredentials(PlatformGLM, AccountTypeAPIKey, credentials))
	require.Error(t, validateAccountCredentials(PlatformGLM, AccountTypeOAuth, credentials))
	require.Error(t, validateAccountCredentials(PlatformGLM, AccountTypeAPIKey, map[string]any{}))
	require.Error(t, validateAccountCredentials(PlatformGLM, AccountTypeAPIKey, map[string]any{
		"api_key": "glm-key", "base_url": "https://relay.example/v1",
	}))

	account := &Account{Platform: PlatformGLM, Type: AccountTypeAPIKey, Credentials: credentials}
	require.True(t, account.IsGLM())
	require.True(t, account.IsOpenAICompatible())
	require.Equal(t, GLMDefaultBaseURL, account.GetOpenAIBaseURL())
	require.False(t, account.ShouldUseOpenAIResponsesAPI())
	require.ElementsMatch(t, []string{"glm-5.2", "glm-5.1", "glm-5", "glm-4.7", "glm-4.6", "glm-4.5-air"}, GLMDefaultModelIDs())

	require.Equal(t, "glm-5.2", defaultOpenAIAccountTestModel(account))
}

func TestGLMUpstreamModelsRequest(t *testing.T) {
	service := &AccountTestService{cfg: &config.Config{Security: config.SecurityConfig{
		URLAllowlist: config.URLAllowlistConfig{Enabled: false},
	}}}
	account := &Account{
		Platform:    PlatformGLM,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "glm-key"},
	}

	req, err := service.buildOpenAIUpstreamModelsRequest(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, http.MethodGet, req.Method)
	require.Equal(t, GLMDefaultBaseURL+"/models", req.URL.String())
	require.Equal(t, "Bearer glm-key", req.Header.Get("Authorization"))
}
