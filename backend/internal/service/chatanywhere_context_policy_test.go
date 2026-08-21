package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type chatAnywhereContextPolicyRepo struct {
	AccountRepository
	calls                  int
	setErrorCalls          int
	tempUnschedulableCalls int
	scope                  string
	resetAt                time.Time
	reason                 string
}

type chatAnywhereContextPolicyBlocker struct {
	clearedIDs []int64
}

func (b *chatAnywhereContextPolicyBlocker) BlockAccountScheduling(*Account, time.Time, string) {}

func (b *chatAnywhereContextPolicyBlocker) ClearAccountSchedulingBlock(accountID int64) {
	b.clearedIDs = append(b.clearedIDs, accountID)
}

func (r *chatAnywhereContextPolicyRepo) SetModelRateLimit(_ context.Context, _ int64, scope string, resetAt time.Time, reason ...string) error {
	r.calls++
	r.scope = scope
	r.resetAt = resetAt
	if len(reason) > 0 {
		r.reason = reason[0]
	}
	return nil
}

func (r *chatAnywhereContextPolicyRepo) SetError(context.Context, int64, string) error {
	r.setErrorCalls++
	return nil
}

func (r *chatAnywhereContextPolicyRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.tempUnschedulableCalls++
	return nil
}

func TestChatAnywhereContextQuotaError(t *testing.T) {
	body := []byte(`{"error":{"message":"当日免费点数不足，已限制模型输入token小于4096"}}`)
	require.True(t, isChatAnywhereContextQuotaExceededError(http.StatusForbidden, "", body))
	require.False(t, isChatAnywhereContextQuotaExceededError(http.StatusBadGateway, "", body))
	require.False(t, isChatAnywhereContextQuotaExceededError(http.StatusForbidden, "access denied", nil))
}

func TestChatAnywhereContextCooldownOnlyBlocksLargeRequests(t *testing.T) {
	resetAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	account := &Account{
		Platform:    PlatformChatAnywhere,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			modelRateLimitsKey: map[string]any{
				chatAnywhereContextRateLimitKey: map[string]any{"rate_limit_reset_at": resetAt},
			},
		},
	}

	small := WithOpenAIRequestInputTokens(context.Background(), chatAnywhereContextLimitTokens, false)
	large := WithOpenAIRequestInputTokens(context.Background(), chatAnywhereContextLimitTokens+1, false)
	require.True(t, account.IsSchedulableForModelWithContext(context.Background(), "gpt-luna-002"))
	require.True(t, account.IsSchedulableForModelWithContext(small, "gpt-luna-002"))
	require.False(t, account.IsSchedulableForModelWithContext(large, "gpt-luna-002"))
	require.Zero(t, account.GetModelRateLimitRemainingTimeWithContext(small, "gpt-luna-002"))
	require.Greater(t, account.GetModelRateLimitRemainingTimeWithContext(large, "gpt-luna-002"), time.Duration(0))
}

func TestChatAnywhereContextQuotaPersistsCooldownWithoutDisabling(t *testing.T) {
	repo := &chatAnywhereContextPolicyRepo{}
	blocker := &chatAnywhereContextPolicyBlocker{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	svc.SetAccountRuntimeBlocker(blocker)
	account := &Account{ID: 42, Platform: PlatformChatAnywhere, Status: StatusActive, Schedulable: true}
	body := []byte(`{"error":{"message":"额度不足够支持本次请求，已限制模型输入token小于4096"}}`)

	shouldDisable := svc.HandleUpstreamError(context.Background(), account, http.StatusForbidden, http.Header{}, body, "gpt-luna-002")

	require.False(t, shouldDisable)
	require.Equal(t, 1, repo.calls)
	require.Zero(t, repo.setErrorCalls)
	require.Zero(t, repo.tempUnschedulableCalls)
	require.Equal(t, chatAnywhereContextRateLimitKey, repo.scope)
	require.Equal(t, "chatanywhere_context_quota_403", repo.reason)
	require.WithinDuration(t, time.Now().Add(chatAnywhereContextCooldown), repo.resetAt, 5*time.Second)
	require.Equal(t, []int64{account.ID}, blocker.clearedIDs)
}

func TestChatAnywhereContextQuotaClearsOpenAIRuntimeBlock(t *testing.T) {
	repo := &chatAnywhereContextPolicyRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	gateway := &OpenAIGatewayService{rateLimitService: svc}
	svc.SetAccountRuntimeBlocker(gateway)
	account := &Account{ID: 43, Platform: PlatformChatAnywhere, Status: StatusActive, Schedulable: true}
	gateway.BlockAccountScheduling(account, time.Time{}, "existing_block")
	require.True(t, gateway.isOpenAIAccountRuntimeBlocked(account))

	shouldDisable := gateway.handleOpenAIAccountUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"当日免费点数不足，已限制模型输入token小于4096"}}`),
		"gpt-luna-002",
	)

	require.False(t, shouldDisable)
	require.False(t, gateway.isOpenAIAccountRuntimeBlocked(account))
	require.Equal(t, StatusActive, account.Status)
	require.True(t, account.Schedulable)
	require.Zero(t, repo.setErrorCalls)
	require.Zero(t, repo.tempUnschedulableCalls)
}

func TestEstimateOpenAICompatibleInputTokensSupportedProtocols(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
	}{
		{
			name:     "chat completions",
			protocol: "chat_completions",
			body:     `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`,
		},
		{
			name:     "responses",
			protocol: "responses",
			body:     `{"model":"gpt-4o","input":"hello"}`,
		},
		{
			name:     "messages",
			protocol: "messages",
			body:     `{"model":"gpt-4o","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := EstimateOpenAICompatibleInputTokens([]byte(tt.body), tt.protocol)
			require.NoError(t, err)
			require.Positive(t, tokens)
		})
	}
}

func TestLargeRequestEstimateFailureFailsClosedForScheduling(t *testing.T) {
	body := make([]byte, chatAnywhereContextLimitTokens+1)
	ctx := WithOpenAICompatibleInputTokensForScheduling(context.Background(), body, "unsupported")

	tokens, ok := OpenAIRequestInputTokensFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, len(body), tokens)
	require.True(t, chatAnywhereContextLimitExceeded(ctx))
}
