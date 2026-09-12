package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

type chatAnywhereUsageLogRepoStub struct {
	UsageLogRepository
	daily   *usagestats.AccountStats
	weekly  *usagestats.AccountStats
	starts  []time.Time
}

var _ UsageLogRepository = (*chatAnywhereUsageLogRepoStub)(nil)

func (r *chatAnywhereUsageLogRepoStub) GetAccountWindowStats(_ context.Context, _ int64, start time.Time) (*usagestats.AccountStats, error) {
	r.starts = append(r.starts, start)
	if time.Since(start) < 48*time.Hour {
		return r.daily, nil
	}
	return r.weekly, nil
}

func TestAccountUsageService_GetChatAnywhereUsageUsesObservedWindows(t *testing.T) {
	repo := &chatAnywhereUsageLogRepoStub{
		daily:  &usagestats.AccountStats{Requests: 17, Tokens: 2100},
		weekly: &usagestats.AccountStats{Requests: 42, Tokens: 12500},
	}
	svc := &AccountUsageService{usageLogRepo: repo}
	account := &Account{ID: 7004, Platform: PlatformChatAnywhere, Type: AccountTypeAPIKey, Status: StatusActive}

	usage, err := svc.getChatAnywhereUsage(context.Background(), account)
	if err != nil {
		t.Fatalf("getChatAnywhereUsage() error = %v", err)
	}
	if usage.Source != "local" {
		t.Fatalf("usage source = %q, want local", usage.Source)
	}
	if usage.ChatAnywhereDaily == nil || usage.ChatAnywhereDaily.UsedRequests != 17 || usage.ChatAnywhereDaily.LimitRequests != 100 {
		t.Fatalf("daily usage = %#v", usage.ChatAnywhereDaily)
	}
	if usage.ChatAnywhereDaily.Utilization != 17 {
		t.Fatalf("daily utilization = %v, want 17", usage.ChatAnywhereDaily.Utilization)
	}
	if usage.ChatAnywhereWeekly == nil || usage.ChatAnywhereWeekly.UsedTokens != 12500 || usage.ChatAnywhereWeekly.LimitTokens != 50000 {
		t.Fatalf("weekly usage = %#v", usage.ChatAnywhereWeekly)
	}
	if usage.ChatAnywhereWeekly.Utilization != 25 {
		t.Fatalf("weekly utilization = %v, want 25", usage.ChatAnywhereWeekly.Utilization)
	}
	if len(repo.starts) != 2 {
		t.Fatalf("window query count = %d, want 2", len(repo.starts))
	}
	if age := time.Since(repo.starts[0]); age < 23*time.Hour || age > 25*time.Hour {
		t.Fatalf("daily window age = %v, want about 24h", age)
	}
	if age := time.Since(repo.starts[1]); age < 167*time.Hour || age > 169*time.Hour {
		t.Fatalf("weekly window age = %v, want about 168h", age)
	}
}

func TestAccountUsageService_GetChatAnywhereUsageMarksProviderPointError(t *testing.T) {
	svc := &AccountUsageService{usageLogRepo: &chatAnywhereUsageLogRepoStub{
		daily:  &usagestats.AccountStats{},
		weekly: &usagestats.AccountStats{},
	}}
	account := &Account{
		ID:           7005,
		Platform:     PlatformChatAnywhere,
		Type:         AccountTypeAPIKey,
		Status:       StatusError,
		ErrorMessage: "HTTP 403: free points in the current 7-day window are insufficient",
	}

	usage, err := svc.getChatAnywhereUsage(context.Background(), account)
	if err != nil {
		t.Fatalf("getChatAnywhereUsage() error = %v", err)
	}
	if usage.ChatAnywhereQuotaStatus != "weekly_exhausted" {
		t.Fatalf("quota status = %q, want weekly_exhausted", usage.ChatAnywhereQuotaStatus)
	}
	if !strings.Contains(account.ErrorMessage, "free points") {
		t.Fatal("test fixture must preserve the provider error text")
	}
}

func TestChatAnywhereUsageProgressClampsNegativeObservedValues(t *testing.T) {
	daily := buildChatAnywhereRequestUsageProgress(&usagestats.AccountStats{Requests: -1})
	weekly := buildChatAnywhereTokenUsageProgress(&usagestats.AccountStats{Tokens: -1})

	if daily.UsedRequests != 0 || daily.Utilization != 0 {
		t.Fatalf("daily progress = %#v, want zero", daily)
	}
	if weekly.UsedTokens != 0 || weekly.Utilization != 0 {
		t.Fatalf("weekly progress = %#v, want zero", weekly)
	}
}
