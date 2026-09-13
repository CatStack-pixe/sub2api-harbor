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
	stats      []*usagestats.AccountStats
	startTimes []time.Time
}

func (r *senseNovaUsageRepo) GetAccountWindowStats(_ context.Context, _ int64, startTime time.Time) (*usagestats.AccountStats, error) {
	r.startTimes = append(r.startTimes, startTime)
	if index := len(r.startTimes) - 1; index < len(r.stats) {
		return r.stats[index], nil
	}
	return &usagestats.AccountStats{}, nil
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

func TestGetUsageForAccount_SenseNovaUsesRollingPointWindows(t *testing.T) {
	repo := &senseNovaUsageRepo{stats: []*usagestats.AccountStats{
		{Requests: 30, Tokens: 30000},
		{Requests: 30, Tokens: 300000},
	}}
	svc := &AccountUsageService{usageLogRepo: repo}
	account := &Account{ID: 42, Platform: PlatformSenseNova, Type: AccountTypeAPIKey}

	usage, err := svc.getUsageForAccount(context.Background(), account, false)
	if err != nil {
		t.Fatalf("getUsageForAccount() error = %v", err)
	}
	if usage == nil || usage.SenseNovaFiveHour == nil || usage.SenseNovaSevenDay == nil {
		t.Fatalf("expected SenseNova rolling point usage, got %#v", usage)
	}
	if usage.SenseNovaFiveHour.Utilization != 50 {
		t.Fatalf("5-hour utilization = %v, want 50", usage.SenseNovaFiveHour.Utilization)
	}
	if usage.SenseNovaSevenDay.Utilization != 50 {
		t.Fatalf("7-day utilization = %v, want 50", usage.SenseNovaSevenDay.Utilization)
	}
	if usage.SenseNovaFiveHour.LimitPoints != senseNovaFiveHourPointsLimit {
		t.Fatalf("5-hour points limit = %d, want %d", usage.SenseNovaFiveHour.LimitPoints, senseNovaFiveHourPointsLimit)
	}
	if usage.SenseNovaSevenDay.LimitPoints != senseNovaWeeklyPointsLimit {
		t.Fatalf("7-day points limit = %d, want %d", usage.SenseNovaSevenDay.LimitPoints, senseNovaWeeklyPointsLimit)
	}
	if usage.SenseNovaFiveHour.UsedPoints != 30000 || usage.SenseNovaSevenDay.UsedPoints != 300000 {
		t.Fatalf("unexpected point usage: (%d, %d)", usage.SenseNovaFiveHour.UsedPoints, usage.SenseNovaSevenDay.UsedPoints)
	}
	if len(repo.startTimes) != 2 {
		t.Fatalf("window queries = %d, want 2", len(repo.startTimes))
	}
	queryNow := time.Now().UTC()
	if got := repo.startTimes[0]; got.Before(queryNow.Add(-senseNovaFiveHourWindow - 2*time.Second)) || got.After(queryNow.Add(-senseNovaFiveHourWindow+2*time.Second)) {
		t.Fatalf("5-hour window start = %v, want roughly now - 5h", got)
	}
	if got := repo.startTimes[1]; got.Before(queryNow.Add(-senseNovaWeeklyWindow - 2*time.Second)) || got.After(queryNow.Add(-senseNovaWeeklyWindow+2*time.Second)) {
		t.Fatalf("7-day window start = %v, want roughly now - 7d", got)
	}
}

func TestBuildSenseNovaUsageProgress_RateLimitFillsWindow(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 34, 45, 0, time.UTC)
	rateLimitReset := now.Add(90 * time.Second)
	account := &Account{RateLimitResetAt: &rateLimitReset}
	stats := &usagestats.AccountStats{Requests: 1, Tokens: 2}

	progress := buildSenseNovaUsageProgress(account, stats, senseNovaFiveHourPointsLimit, now)
	if progress.Utilization != 100 {
		t.Fatalf("rate-limited utilization = %v, want 100", progress.Utilization)
	}
	if progress.ResetsAt == nil || !progress.ResetsAt.Equal(rateLimitReset) {
		t.Fatalf("rolling point reset = %v, want %v", progress.ResetsAt, rateLimitReset)
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
