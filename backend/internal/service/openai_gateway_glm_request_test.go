package service

import (
	"net/http"
	"testing"

	"github.com/tidwall/gjson"
	"github.com/stretchr/testify/require"
)

func TestNormalizeGLMChatCompletionsRequestBody(t *testing.T) {
	body := []byte(`{"model":"glm-5.3","prompt_cache_key":"cache-1","promptCacheKey":"cache-2","reasoning":{"effort":"xhigh","trace":"keep"}}`)

	updated, changed, err := normalizeGLMChatCompletionsRequestBody(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, gjson.ValidBytes(updated))
	require.False(t, gjson.GetBytes(updated, "prompt_cache_key").Exists())
	require.False(t, gjson.GetBytes(updated, "promptCacheKey").Exists())
	require.False(t, gjson.GetBytes(updated, "reasoning.effort").Exists())
	require.Equal(t, "max", gjson.GetBytes(updated, "reasoning_effort").String())
	require.Equal(t, "keep", gjson.GetBytes(updated, "reasoning.trace").String())
}

func TestNormalizeGLMChatCompletionsRequestBodyLeavesNonGLMModelUntouched(t *testing.T) {
	body := []byte(`{"model":"gpt-5.2","prompt_cache_key":"cache-1"}`)
	updated, changed, err := normalizeGLMChatCompletionsRequestBody(body)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, string(body), string(updated))
}

func TestFailoverDeepSeekTransientStatusesRetrySameAccountInPool(t *testing.T) {
	account := &Account{
		ID:       41,
		Platform: PlatformDeepSeek,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"pool_mode": true,
		},
	}
	service := &OpenAIGatewayService{}
	for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			resp := &http.Response{StatusCode: status, Header: make(http.Header)}
			err := service.failoverOpenAIUpstreamHTTPError(nil, nil, account, resp, []byte(`{"error":{"message":"temporary"}}`), "temporary", "deepseek-v4-pro")
			require.NotNil(t, err)
			require.True(t, err.RetryableOnSameAccount)
		})
	}
}

func TestDeepSeekDeterministicErrorsDoNotRetrySameAccount(t *testing.T) {
	account := &Account{
		Platform: PlatformDeepSeek,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"pool_mode": true,
		},
	}
	for _, status := range []int{http.StatusTooManyRequests, http.StatusBadRequest, http.StatusUnauthorized} {
		require.False(t, openAIAccountRetryableOnSameAccount(account, status, "invalid parameter", nil, false))
	}
	require.False(t, openAIAccountRetryableOnSameAccount(account, http.StatusServiceUnavailable, "model not found", nil, false))
}
