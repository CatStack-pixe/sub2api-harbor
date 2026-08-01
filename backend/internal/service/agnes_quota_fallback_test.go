//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAgnesInsufficientQuotaResponseRequiresExactCode(t *testing.T) {
	account := &Account{Platform: PlatformAgnes}
	require.True(t, isAgnesInsufficientQuotaResponse(account, http.StatusForbidden, []byte(`{"error":{"code":"insufficient_user_quota"}}`)))
	require.True(t, isAgnesInsufficientQuotaResponse(account, http.StatusPaymentRequired, []byte(`{"error":{"code":"INSUFFICIENT_USER_QUOTA"}}`)))
	require.False(t, isAgnesInsufficientQuotaResponse(account, http.StatusForbidden, []byte(`{"error":{"code":"invalid_api_key"}}`)))
	require.False(t, isAgnesInsufficientQuotaResponse(&Account{Platform: PlatformOpenAI}, http.StatusForbidden, []byte(`{"error":{"code":"insufficient_user_quota"}}`)))
}

func TestNextAgnesQuotaResetUsesSingaporeMidnight(t *testing.T) {
	now := time.Date(2026, time.August, 2, 23, 59, 30, 0, time.FixedZone("test", 8*60*60))
	want := time.Date(2026, time.August, 3, 0, 0, 0, 0, agnesQuotaResetLocation)
	require.Equal(t, want, nextAgnesQuotaReset(now))
}

func TestForwardAsRawChatCompletions_AgnesQuotaFallsBackUntilReset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	quotaBody := `{"error":{"message":"precharge failed","type":"AgnesAI_error","code":"insufficient_user_quota"}}`
	successBody := `{"id":"chatcmpl_agnes","object":"chat.completion","model":"agnes-2.0-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(quotaBody))},
		{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(successBody))},
		{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(successBody))},
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID: 201, Name: "agnes", Platform: PlatformAgnes, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "test", "base_url": "http://upstream.example/v1"},
	}

	firstRecorder := httptest.NewRecorder()
	firstContext, _ := gin.CreateTestContext(firstRecorder)
	firstContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	first, err := svc.forwardAsRawChatCompletions(context.Background(), firstContext, account, body, "agnes-2.5-pro-alpha")
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, AgnesDefaultModel, first.BillingModel)
	require.Equal(t, AgnesDefaultModel, first.UpstreamModel)
	require.Len(t, upstream.bodies, 2)
	require.Equal(t, "agnes-2.5-pro-alpha", gjson.GetBytes(upstream.bodies[0], "model").String())
	require.Equal(t, AgnesDefaultModel, gjson.GetBytes(upstream.bodies[1], "model").String())

	secondRecorder := httptest.NewRecorder()
	secondContext, _ := gin.CreateTestContext(secondRecorder)
	secondContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	second, err := svc.forwardAsRawChatCompletions(context.Background(), secondContext, account, body, "agnes-2.5-pro-alpha")
	require.NoError(t, err)
	require.NotNil(t, second)
	require.Len(t, upstream.bodies, 3)
	require.Equal(t, AgnesDefaultModel, gjson.GetBytes(upstream.bodies[2], "model").String())
}

func TestAgnesInsufficientQuotaDoesNotDisableAccount(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	rateLimitService := &RateLimitService{accountRepo: repo}
	svc := &OpenAIGatewayService{rateLimitService: rateLimitService}
	account := &Account{ID: 202, Platform: PlatformAgnes, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}

	disabled := svc.handleOpenAIAccountUpstreamError(
		context.Background(), account, http.StatusForbidden, http.Header{},
		[]byte(`{"error":{"code":"insufficient_user_quota","message":"precharge failed"}}`), AgnesDefaultModel,
	)

	require.False(t, disabled)
	require.Zero(t, repo.setErrorCalls)
	require.Equal(t, StatusActive, account.Status)
}
