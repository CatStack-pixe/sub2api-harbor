//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestKimiAccountCredentialsAndBaseURLs(t *testing.T) {
	for _, credentials := range []map[string]any{
		{"api_key": "kimi-key"},
		{"api_key": "kimi-key", "base_url": KimiDefaultBaseURL},
		{"api_key": "kimi-key", "base_url": KimiInternationalBaseURL + "/"},
		{"api_key": "kimi-key", "base_url": "https://relay.example/v1"},
	} {
		require.NoError(t, validateAccountCredentials(PlatformKimi, AccountTypeAPIKey, credentials))
	}

	for _, credentials := range []map[string]any{
		{},
		{"api_key": "kimi-key", "base_url": "relay.example/v1"},
		{"api_key": "kimi-key", "base_url": "ftp://relay.example/v1"},
		{"api_key": "kimi-key", "base_url": 42},
	} {
		err := validateAccountCredentials(PlatformKimi, AccountTypeAPIKey, credentials)
		require.Error(t, err)
	}
	require.Error(t, validateAccountCredentials(PlatformKimi, AccountTypeOAuth, map[string]any{"api_key": "kimi-key"}))

	cn := &Account{Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "kimi-key"}}
	intl := &Account{Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "kimi-key", "base_url": KimiInternationalBaseURL + "/"}}
	legacy := &Account{Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "kimi-key", "base_url": "https://relay.example/v1"}}
	require.Equal(t, KimiDefaultBaseURL, cn.GetOpenAIBaseURL())
	require.Equal(t, KimiInternationalBaseURL, intl.GetOpenAIBaseURL())
	require.Equal(t, KimiDefaultBaseURL, legacy.GetOpenAIBaseURL())
}

func TestParseKimiBalanceResponse(t *testing.T) {
	result, err := ParseKimiBalanceResponse([]byte(`{"code":0,"status":true,"data":{"available_balance":12.5,"voucher_balance":2.25,"cash_balance":10.25}}`))
	require.NoError(t, err)
	require.True(t, result.IsAvailable)
	require.Equal(t, 12.5, result.AvailableBalance)
	require.Equal(t, 2.25, result.VoucherBalance)
	require.Equal(t, 10.25, result.CashBalance)
	require.Empty(t, result.Currency)

	_, err = ParseKimiBalanceResponse([]byte(`not json`))
	require.Error(t, err)
	_, err = ParseKimiBalanceResponse([]byte(`{"code":"INVALID_KEY","status":false,"message":"expired","data":{}}`))
	require.Error(t, err)
	require.Equal(t, http.StatusBadGateway, infraerrors.Code(err))
}

func TestKimiBalanceCurrencyMatchesRegion(t *testing.T) {
	require.Equal(t, "CNY", kimiBalanceCurrency(KimiDefaultBaseURL))
	require.Equal(t, "USD", kimiBalanceCurrency(KimiInternationalBaseURL+"/"))
}

func TestFetchKimiBalancePreservesProviderStatus(t *testing.T) {
	account := &Account{
		ID:       951,
		Platform: PlatformKimi,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "kimi-key",
			"base_url": KimiInternationalBaseURL,
		},
	}
	for _, status := range []int{http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusTooManyRequests} {
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

		_, err := svc.FetchKimiBalance(context.Background(), account.ID)
		require.Error(t, err)
		require.Equal(t, status, infraerrors.Code(err))
		require.Equal(t, "KIMI_BALANCE_UPSTREAM_ERROR", infraerrors.Reason(err))
		require.Equal(t, KimiInternationalBaseURL+"/users/me/balance", upstream.lastReq.URL.String())
		require.Equal(t, "Bearer kimi-key", upstream.lastReq.Header.Get("Authorization"))
	}
}

func TestFetchKimiBalanceDoesNotMutateSchedulingState(t *testing.T) {
	account := &Account{
		ID:          952,
		Platform:    PlatformKimi,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"api_key": "kimi-key"},
		Extra:       map[string]any{"quota_used": 3.5},
	}
	repo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"code":0,"status":true,"data":{"available_balance":9.5,"voucher_balance":1.5,"cash_balance":8}}`)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
	}

	result, err := svc.FetchKimiBalance(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, 9.5, result.AvailableBalance)
	require.Equal(t, "CNY", result.Currency)
	require.Equal(t, KimiDefaultBaseURL+"/users/me/balance", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer kimi-key", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, StatusActive, account.Status)
	require.True(t, account.Schedulable)
	require.Equal(t, 3.5, account.Extra["quota_used"])
}
