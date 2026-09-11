package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

type senseNovaUsageRepo struct {
	UsageLogRepository
	stats     *usagestats.AccountStats
	startTime time.Time
}

func (r *senseNovaUsageRepo) GetAccountWindowStats(_ context.Context, _ int64, startTime time.Time) (*usagestats.AccountStats, error) {
	r.startTime = startTime
	return r.stats, nil
}

type senseNovaRateLimitRepo struct {
	AccountRepository
	resetAt time.Time
	calls   int
}

func (r *senseNovaRateLimitRepo) SetRateLimited(_ context.Context, _ int64, resetAt time.Time) error {
	r.resetAt = resetAt
	r.calls++
	return nil
}

func TestGetUsageForAccount_SenseNovaUsesLocalRPMAndTPMWindows(t *testing.T) {
	repo := &senseNovaUsageRepo{stats: &usagestats.AccountStats{Requests: 30, Tokens: 64000}}
	svc := &AccountUsageService{usageLogRepo: repo}
	account := &Account{ID: 42, Platform: PlatformSenseNova, Type: AccountTypeAPIKey}

	usage, err := svc.getUsageForAccount(context.Background(), account, false)
	if err != nil {
		t.Fatalf("getUsageForAccount() error = %v", err)
	}
	if usage == nil || usage.SenseNovaRPM == nil || usage.SenseNovaTPM == nil {
		t.Fatalf("expected SenseNova RPM/TPM usage, got %#v", usage)
	}
	if usage.SenseNovaRPM.Utilization != 50 {
		t.Fatalf("RPM utilization = %v, want 50", usage.SenseNovaRPM.Utilization)
	}
	if usage.SenseNovaTPM.Utilization != 50 {
		t.Fatalf("TPM utilization = %v, want 50", usage.SenseNovaTPM.Utilization)
	}
	if usage.SenseNovaRPM.LimitRequests != senseNovaRPMLimit {
		t.Fatalf("RPM limit = %d, want %d", usage.SenseNovaRPM.LimitRequests, senseNovaRPMLimit)
	}
	if usage.SenseNovaRPM.WindowStats == nil || usage.SenseNovaRPM.WindowStats.Tokens != 64000 {
		t.Fatalf("unexpected local window stats: %#v", usage.SenseNovaRPM.WindowStats)
	}
	if repo.startTime.IsZero() {
		t.Fatal("expected local minute window query")
	}
}

func TestBuildSenseNovaUsageProgress_RateLimitFillsBothWindows(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 34, 45, 0, time.UTC)
	rateLimitReset := now.Add(90 * time.Second)
	account := &Account{RateLimitResetAt: &rateLimitReset}
	stats := &usagestats.AccountStats{Requests: 1, Tokens: 2}

	rpm, tpm := buildSenseNovaUsageProgress(account, stats, now)
	if rpm.Utilization != 100 || tpm.Utilization != 100 {
		t.Fatalf("rate-limited utilization = (%v, %v), want (100, 100)", rpm.Utilization, tpm.Utilization)
	}
	if rpm.ResetsAt == nil || !rpm.ResetsAt.Equal(rateLimitReset) {
		t.Fatalf("RPM reset = %v, want %v", rpm.ResetsAt, rateLimitReset)
	}
	if tpm.ResetsAt == nil || !tpm.ResetsAt.Equal(rateLimitReset) {
		t.Fatalf("TPM reset = %v, want %v", tpm.ResetsAt, rateLimitReset)
	}
}

func TestHandleUpstreamError_SenseNova429PersistsRateLimitBeforePoolModeReturn(t *testing.T) {
	repo := &senseNovaRateLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{
		ID:       7,
		Platform: PlatformSenseNova,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"pool_mode": true,
		},
	}

	before := time.Now().Add(80 * time.Second)
	if got := svc.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests, http.Header{"Retry-After": []string{"90"}}, nil); got {
		t.Fatal("expected 429 to remain retryable")
	}
	after := time.Now().Add(100 * time.Second)
	if repo.calls != 1 {
		t.Fatalf("SetRateLimited calls = %d, want 1", repo.calls)
	}
	if repo.resetAt.Before(before) || repo.resetAt.After(after) {
		t.Fatalf("resetAt = %v, expected roughly 90 seconds from now", repo.resetAt)
	}
	if account.RateLimitResetAt == nil || !account.RateLimitResetAt.Equal(repo.resetAt) {
		t.Fatalf("account reset = %v, want persisted reset %v", account.RateLimitResetAt, repo.resetAt)
	}
}

func TestSenseNovaRateLimitResetAtUsesMinuteBoundaryWithoutLongerRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 34, 45, 0, time.UTC)
	want := time.Date(2026, 9, 12, 12, 35, 0, 0, time.UTC)

	if got := senseNovaRateLimitResetAt(nil, now); !got.Equal(want) {
		t.Fatalf("resetAt = %v, want %v", got, want)
	}
	if got := senseNovaRateLimitResetAt(http.Header{"Retry-After": []string{"90"}}, now); !got.Equal(now.Add(90 * time.Second)) {
		t.Fatalf("long Retry-After resetAt = %v, want %v", got, now.Add(90*time.Second))
	}
}
