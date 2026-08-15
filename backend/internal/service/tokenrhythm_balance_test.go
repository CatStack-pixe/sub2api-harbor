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

func TestTokenRhythmCookieCredentials(t *testing.T) {
	session, csrf, err := TokenRhythmCookieCredentials(map[string]any{
		"tokenrhythm_cookie": "theme=dark; tr_csrf=csrf-value; tr_session=sess-value; locale=zh-CN",
	})
	require.NoError(t, err)
	require.Equal(t, "sess-value", session)
	require.Equal(t, "csrf-value", csrf)

	for _, value := range []string{
		"tr_session=sess-value",
		"tr_session=sess; tr_csrf=csrf; tr_session=duplicate",
		"tr_session=sess\r\nCookie: tr_csrf=csrf",
	} {
		_, _, err := TokenRhythmCookieCredentials(map[string]any{"tokenrhythm_cookie": value})
		require.Error(t, err, value)
	}
}

func TestSanitizeTokenRhythmCredentials(t *testing.T) {
	credentials := SanitizeStoredCredentials(PlatformTokenRhythm, map[string]any{
		"api_key":            "tr-api-key",
		"base_url":           "https://untrusted.example/v1",
		"tokenrhythm_cookie": "tr_session=session; tr_csrf=csrf; other=ignored",
	})
	require.Equal(t, TokenRhythmDefaultBaseURL, credentials["base_url"])
	require.Equal(t, "session", credentials["tr_session"])
	require.Equal(t, "csrf", credentials["tr_csrf"])
	require.NotContains(t, credentials, "tokenrhythm_cookie")
	require.True(t, IsSensitiveCredentialKey("tr_session"))
	require.True(t, IsSensitiveCredentialKey("tr_csrf"))
}

func TestValidateTokenRhythmCredentials(t *testing.T) {
	valid := map[string]any{
		"api_key":            "tr-api-key",
		"tokenrhythm_cookie": "tr_session=session; tr_csrf=csrf",
	}
	require.NoError(t, validateAccountCredentials(PlatformTokenRhythm, AccountTypeAPIKey, valid))
	require.Error(t, validateAccountCredentials(PlatformTokenRhythm, AccountTypeOAuth, valid))
	require.Error(t, validateAccountCredentials(PlatformTokenRhythm, AccountTypeAPIKey, map[string]any{
		"api_key": "tr-api-key",
		"base_url": "https://relay.example/v1",
		"tokenrhythm_cookie": "tr_session=session; tr_csrf=csrf",
	}))
}

func TestParseTokenRhythmBalanceResponse(t *testing.T) {
	result, err := ParseTokenRhythmBalanceResponse([]byte(`{
		"code":0,
		"data":{"calls":186,"successCalls":184,"errorCalls":1,"abortedCalls":1,"inputTokens":35080279,"outputTokens":91965,"costCny":70.5,"balanceCny":609.49,"frozenBalanceCny":0,"availableBalanceCny":609.49,"expiringBalanceCny":609.49,"nextExpiryAt":"2026-09-09T03:03:13Z","currency":"CNY"}
	}`))
	require.NoError(t, err)
	require.True(t, result.IsAvailable)
	require.Equal(t, 609.49, result.AvailableBalanceCNY)
	require.Equal(t, int64(35080279), result.InputTokens)

	_, err = ParseTokenRhythmBalanceResponse([]byte(`{"code":"UNAUTHORIZED","message":"expired"}`))
	require.Error(t, err)
	require.Equal(t, http.StatusUnauthorized, infraerrors.Code(err))
}

func TestFetchTokenRhythmBalanceDoesNotMutateSchedulingState(t *testing.T) {
	account := &Account{
		ID:         941,
		Platform:   PlatformTokenRhythm,
		Type:       AccountTypeAPIKey,
		Status:     StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"api_key":    "tr-api-key",
			"tr_session": "session-value",
			"tr_csrf":    "csrf-value",
		},
	}
	repo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"code":0,"data":{"availableBalanceCny":9.5,"currency":"CNY"}}`)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
	}

	result, err := svc.FetchTokenRhythmBalance(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, 9.5, result.AvailableBalanceCNY)
	require.Equal(t, tokenRhythmBalanceURL, upstream.lastReq.URL.String())
	require.Equal(t, "tr_session=session-value; tr_csrf=csrf-value", upstream.lastReq.Header.Get("Cookie"))
	require.Empty(t, upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, StatusActive, account.Status)
	require.True(t, account.Schedulable)
}
