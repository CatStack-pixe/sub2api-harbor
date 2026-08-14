//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type runtimeBlockRecorder struct {
	accounts   []*Account
	until      []time.Time
	reasons    []string
	clearedIDs []int64
}

func (r *runtimeBlockRecorder) BlockAccountScheduling(account *Account, until time.Time, reason string) {
	r.accounts = append(r.accounts, account)
	r.until = append(r.until, until)
	r.reasons = append(r.reasons, reason)
}

func (r *runtimeBlockRecorder) ClearAccountSchedulingBlock(accountID int64) {
	r.clearedIDs = append(r.clearedIDs, accountID)
}

func TestRateLimitService_HandleUpstreamError_OpenAI403FirstHitTempUnschedulable(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	counter := &openAI403CounterCacheStub{counts: []int64{1}}
	blocker := &runtimeBlockRecorder{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAI403CounterCache(counter)
	service.SetAccountRuntimeBlocker(blocker)
	account := &Account{
		ID:       301,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"temporary edge rejection"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.Contains(t, repo.lastTempReason, "temporary edge rejection")
	require.Contains(t, repo.lastTempReason, "(1/3)")
	require.Len(t, blocker.accounts, 1)
	require.Equal(t, account.ID, blocker.accounts[0].ID)
	require.Equal(t, "openai_403_temp", blocker.reasons[0])
	require.True(t, blocker.until[0].After(time.Now()))
}

func TestRateLimitService_HandleUpstreamError_OpenAI403ThresholdDisables(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	counter := &openAI403CounterCacheStub{counts: []int64{3}}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAI403CounterCache(counter)
	account := &Account{
		ID:       302,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"workspace forbidden by policy"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Equal(t, 0, repo.tempCalls)
	require.Contains(t, repo.lastErrorMsg, "workspace forbidden by policy")
	require.Contains(t, repo.lastErrorMsg, "consecutive_403=3/3")
}

func poolModeInsufficientBalanceAccount(customCodes bool) *Account {
	return &Account{
		ID:       303,
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"pool_mode":                   true,
			"custom_error_codes_enabled": customCodes,
		},
	}
}

func TestRateLimitService_PoolModeInsufficientBalanceTemporarilyUnschedules(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	blocker := &runtimeBlockRecorder{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetAccountRuntimeBlocker(blocker)
	account := poolModeInsufficientBalanceAccount(false)
	body := []byte(`{"error":{"message":"Insufficient Account Balance for this request"}}`)

	require.Equal(t, ErrorPolicyTempUnscheduled, service.CheckErrorPolicy(context.Background(), account, http.StatusForbidden, body))
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, poolModeInsufficientBalanceReason, repo.lastTempReason)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Len(t, blocker.accounts, 1)
	require.Equal(t, poolModeInsufficientBalanceReason, blocker.reasons[0])
	require.True(t, blocker.until[0].After(time.Now().Add(29*time.Minute)))

	// The forwarding path uses the same detector and fails over immediately.
	require.True(t, service.HandleUpstreamError(context.Background(), account, http.StatusForbidden, http.Header{}, body))
	require.Equal(t, 2, repo.tempCalls)
	require.Equal(t, 0, repo.setErrorCalls)
}

func TestRateLimitService_PoolModeInsufficientBalanceNarrowMatching(t *testing.T) {
	tests := []struct {
		name       string
		account    *Account
		statusCode int
		body       []byte
		wantPolicy ErrorPolicyResult
		wantCalls  int
	}{
		{
			name:       "generic pool 403 remains skipped",
			account:    poolModeInsufficientBalanceAccount(false),
			statusCode: http.StatusForbidden,
			body:       []byte(`{"error":{"message":"forbidden"}}`),
			wantPolicy: ErrorPolicySkipped,
		},
		{
			name:       "custom error codes take precedence",
			account:    poolModeInsufficientBalanceAccount(true),
			statusCode: http.StatusForbidden,
			body:       []byte(`{"error":{"message":"insufficient account balance"}}`),
			wantPolicy: ErrorPolicySkipped,
		},
		{
			name:       "non pool account unchanged",
			account: &Account{ID: 304, Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
			statusCode: http.StatusForbidden,
			body:       []byte(`insufficient account balance`),
			wantPolicy: ErrorPolicyNone,
		},
		{
			name:       "other balance wording unchanged",
			account:    poolModeInsufficientBalanceAccount(false),
			statusCode: http.StatusForbidden,
			body:       []byte(`{"error":{"message":"insufficient balance"}}`),
			wantPolicy: ErrorPolicySkipped,
		},
		{
			name:       "other status unchanged",
			account:    poolModeInsufficientBalanceAccount(false),
			statusCode: http.StatusTooManyRequests,
			body:       []byte(`insufficient account balance`),
			wantPolicy: ErrorPolicySkipped,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			require.Equal(t, tc.wantPolicy, service.CheckErrorPolicy(context.Background(), tc.account, tc.statusCode, tc.body))
			require.Equal(t, tc.wantCalls, repo.tempCalls)
			require.Equal(t, 0, repo.setErrorCalls)
		})
	}
}
