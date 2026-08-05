//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestParseDeepSeekBalanceResponse(t *testing.T) {
	result, err := ParseDeepSeekBalanceResponse([]byte(`{
		"is_available":true,
		"balance_infos":[
			{"currency":"CNY","total_balance":"10.50","granted_balance":"2.00","topped_up_balance":"8.50"},
			{"currency":"USD","total_balance":1.25,"granted_balance":0,"topped_up_balance":1.25}
		]
	}`))

	require.NoError(t, err)
	require.True(t, result.IsAvailable)
	require.Equal(t, []DeepSeekBalanceInfo{
		{Currency: "CNY", TotalBalance: "10.50", GrantedBalance: "2.00", ToppedUpBalance: "8.50"},
		{Currency: "USD", TotalBalance: "1.25", GrantedBalance: "0", ToppedUpBalance: "1.25"},
	}, result.BalanceInfos)
}

func TestParseDeepSeekBalanceResponseRejectsInvalidAmounts(t *testing.T) {
	for _, body := range []string{
		`{"balance_infos":[{"currency":"CNY","total_balance":"not-a-number"}]}`,
		`{"balance_infos":[{"currency":"CNY","total_balance":true}]}`,
	} {
		_, err := ParseDeepSeekBalanceResponse([]byte(body))
		require.Error(t, err)
	}
}

func TestBuildDeepSeekBalanceURL(t *testing.T) {
	require.Equal(t, "https://api.deepseek.com/user/balance", buildDeepSeekBalanceURL("https://api.deepseek.com"))
	require.Equal(t, "https://relay.example/v1/user/balance", buildDeepSeekBalanceURL("https://relay.example/v1/"))
	require.Equal(t, "https://relay.example/user/balance", buildDeepSeekBalanceURL("https://relay.example/user/balance"))
}

func TestBuildDeepSeekModelsRequest(t *testing.T) {
	svc := &AccountTestService{cfg: &config.Config{Security: config.SecurityConfig{
		URLAllowlist: config.URLAllowlistConfig{Enabled: false},
	}}}
	req, err := svc.buildUpstreamModelsRequest(context.Background(), &Account{
		Platform: PlatformDeepSeek,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-deepseek",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "https://api.deepseek.com/models", req.URL.String())
	require.Equal(t, "Bearer sk-deepseek", req.Header.Get("Authorization"))
}

func TestFetchDeepSeekBalancePreservesProviderStatus(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusTooManyRequests} {
		account := &Account{
			ID:       901,
			Platform: PlatformDeepSeek,
			Type:     AccountTypeAPIKey,
			Credentials: map[string]any{
				"api_key":  "sk-deepseek",
				"base_url": "https://relay.example/v1",
			},
		}
		repo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
		upstream := &httpUpstreamRecorder{resp: &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(`{"error":"provider failure"}`)),
		}}
		svc := &AccountTestService{
			accountRepo:  repo,
			httpUpstream: upstream,
			cfg: &config.Config{Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{Enabled: false},
			}},
		}

		_, err := svc.FetchDeepSeekBalance(context.Background(), account.ID)
		require.Error(t, err)
		require.Equal(t, status, infraerrors.Code(err))
		require.Equal(t, "DEEPSEEK_BALANCE_UPSTREAM_ERROR", infraerrors.Reason(err))
		require.Equal(t, "https://relay.example/v1/user/balance", upstream.lastReq.URL.String())
		require.Equal(t, "Bearer sk-deepseek", upstream.lastReq.Header.Get("Authorization"))
	}
}

func TestFetchDeepSeekBalanceDoesNotMutateSchedulingState(t *testing.T) {
	account := &Account{
		ID:          902,
		Platform:     PlatformDeepSeek,
		Type:         AccountTypeAPIKey,
		Status:       StatusActive,
		Schedulable:  true,
		Credentials:  map[string]any{"api_key": "sk-deepseek"},
		Extra:        map[string]any{"quota_used": 3.5},
		Concurrency:  1,
	}
	repo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(bytes.NewBufferString(`{
			"is_available":true,
			"balance_infos":[{"currency":"CNY","total_balance":"12.00","granted_balance":"4.00","topped_up_balance":"8.00"}]
		}`)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
	}

	result, err := svc.FetchDeepSeekBalance(context.Background(), account.ID)
	require.NoError(t, err)
	require.True(t, result.IsAvailable)
	require.Equal(t, "12.00", result.BalanceInfos[0].TotalBalance)
	require.Equal(t, 3.5, account.Extra["quota_used"])
	require.True(t, account.Schedulable)
}

func TestOpenAIGatewayDeepSeekChatCompletionsUsesRawEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{
		"model":"deepseek-reasoner",
		"messages":[
			{"role":"user","content":"weather"},
			{"role":"assistant","reasoning_content":"need tool","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"cloudy"}
		],
		"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}],
		"tool_choice":"auto",
		"response_format":{"type":"json_object"},
		"stream":false,
		"user_id":"user-1"
	}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(bytes.NewBufferString(`{"id":"chatcmpl_deepseek","object":"chat.completion","model":"deepseek-reasoner","choices":[{"index":0,"message":{"role":"assistant","reasoning_content":"thinking","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID:          903,
		Platform:    PlatformDeepSeek,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-deepseek", "base_url": "https://relay.example/v1"},
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "https://relay.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-deepseek", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "need tool", gjson.GetBytes(upstream.lastBody, "messages.1.reasoning_content").String())
	require.Equal(t, "get_weather", gjson.GetBytes(upstream.lastBody, "tools.0.function.name").String())
	require.Equal(t, "auto", gjson.GetBytes(upstream.lastBody, "tool_choice").String())
	require.Equal(t, "json_object", gjson.GetBytes(upstream.lastBody, "response_format.type").String())
	require.Equal(t, "user-1", gjson.GetBytes(upstream.lastBody, "user_id").String())
	require.Equal(t, "thinking", gjson.Get(rec.Body.String(), "choices.0.message.reasoning_content").String())
}
