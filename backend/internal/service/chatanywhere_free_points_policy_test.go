package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type chatAnywhereFreePointsPolicyRepo struct {
	AccountRepository
	tempCalls      int
	setErrorCalls  int
	lastTempUntil  time.Time
	lastTempReason string
}

func (r *chatAnywhereFreePointsPolicyRepo) SetTempUnschedulable(_ context.Context, _ int64, until time.Time, reason string) error {
	r.tempCalls++
	r.lastTempUntil = until
	r.lastTempReason = reason
	return nil
}

func (r *chatAnywhereFreePointsPolicyRepo) SetError(context.Context, int64, string) error {
	r.setErrorCalls++
	return nil
}

func TestIsChatAnywhereFreePointsQuotaExceededError(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
		body    string
		want    bool
	}{
		{
			name:   "chinese weekly quota",
			status: http.StatusForbidden,
			body:   `{"error":{"message":"7天免费点数不足以支持本次请求"}}`,
			want:   true,
		},
		{
			name:   "english weekly quota",
			status: http.StatusForbidden,
			body:   `{"error":{"message":"The free points available in the current 7-day window are insufficient"}}`,
			want:   true,
		},
		{
			name:   "context quota is model scoped",
			status: http.StatusForbidden,
			body:   `{"error":{"message":"当日免费点数不足，已限制模型输入token小于4096"}}`,
			want:   false,
		},
		{
			name:   "wrong status",
			status: http.StatusUnauthorized,
			body:   `{"error":{"message":"7天免费点数不足以支持本次请求"}}`,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isChatAnywhereFreePointsQuotaExceededError(tt.status, tt.message, []byte(tt.body)))
		})
	}
}

func TestHandleUpstreamError_ChatAnywhereFreePointsQuotaIsTemporary(t *testing.T) {
	repo := &chatAnywhereFreePointsPolicyRepo{}
	service := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{
		ID:          6306,
		Platform:    PlatformChatAnywhere,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	body := []byte(`{"error":{"message":"7天免费点数不足以支持本次请求，请等待当前额度窗口结束"}}`)

	shouldDisable := service.HandleUpstreamError(context.Background(), account, http.StatusForbidden, http.Header{}, body)

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.tempCalls)
	require.Zero(t, repo.setErrorCalls)
	require.Equal(t, StatusActive, account.Status)
	require.True(t, account.Schedulable)
	require.Contains(t, repo.lastTempReason, chatAnywhereFreePointsQuotaReasonPrefix)
	require.WithinDuration(t, time.Now().Add(chatAnywhereFreePointsWindow), repo.lastTempUntil, 5*time.Second)
}
