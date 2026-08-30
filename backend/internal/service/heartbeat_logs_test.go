package service

import (
	"context"
	"testing"
	"time"
)

type heartbeatLogRepoStub struct {
	page     int
	pageSize int
}

func (r *heartbeatLogRepoStub) Enqueue(context.Context, HeartbeatProvisioningEnqueueInput) error {
	return nil
}
func (r *heartbeatLogRepoStub) Claim(context.Context, string, time.Duration) (*HeartbeatProvisioningJob, error) {
	return nil, nil
}
func (r *heartbeatLogRepoStub) SetProxy(context.Context, int64, int64) error   { return nil }
func (r *heartbeatLogRepoStub) SetAccount(context.Context, int64, int64) error { return nil }
func (r *heartbeatLogRepoStub) FindPendingAccountByFingerprint(context.Context, string) (*int64, error) {
	return nil, nil
}
func (r *heartbeatLogRepoStub) Complete(context.Context, int64) error { return nil }
func (r *heartbeatLogRepoStub) Retry(context.Context, int64, int, time.Time, bool, string) error {
	return nil
}
func (r *heartbeatLogRepoStub) Stats(context.Context) (*HeartbeatQueueStats, error) {
	return &HeartbeatQueueStats{}, nil
}
func (r *heartbeatLogRepoStub) ListLogs(_ context.Context, page, pageSize int) (*HeartbeatProvisioningLogList, error) {
	r.page, r.pageSize = page, pageSize
	return &HeartbeatProvisioningLogList{Logs: []*HeartbeatProvisioningLog{{ID: 7}}, Total: 1, Page: page, PageSize: pageSize}, nil
}

func TestHeartbeatProvisioningListLogsNormalizesPagination(t *testing.T) {
	repo := &heartbeatLogRepoStub{}
	service := &HeartbeatProvisioningService{repo: repo}

	result, err := service.ListLogs(context.Background(), 0, 999)
	if err != nil {
		t.Fatalf("ListLogs() error = %v", err)
	}
	if repo.page != 1 || repo.pageSize != 200 {
		t.Fatalf("repo pagination = %d/%d, want 1/200", repo.page, repo.pageSize)
	}
	if result == nil || len(result.Logs) != 1 || result.Logs[0].ID != 7 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestHeartbeatProvisioningListLogsWithoutRepository(t *testing.T) {
	result, err := (&HeartbeatProvisioningService{}).ListLogs(context.Background(), 2, 10)
	if err != nil {
		t.Fatalf("ListLogs() error = %v", err)
	}
	if result == nil || result.Total != 0 || result.Page != 2 || result.PageSize != 10 || result.Logs == nil {
		t.Fatalf("unexpected empty result: %+v", result)
	}
}
