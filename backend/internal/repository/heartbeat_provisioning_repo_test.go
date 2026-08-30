package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestHeartbeatProvisioningRepositoryListLogs(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM heartbeat_provision_jobs")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	updatedAt := createdAt.Add(time.Minute)
	completedAt := updatedAt.Add(time.Minute)
	mock.ExpectQuery(`(?s)SELECT id, provider, fingerprint, source_balance.*FROM heartbeat_provision_jobs.*LIMIT \$1 OFFSET \$2`).
		WithArgs(200, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "provider", "fingerprint", "source_balance", "source_checked_at", "status",
			"attempts", "available_at", "locked_until", "target_group_id", "target_proxy_group_id",
			"account_id", "proxy_id", "last_error", "created_at", "updated_at", "completed_at",
		}).AddRow(
			int64(9), "ds", "0123456789abcdef01234567", 12.5, createdAt, "complete",
			2, createdAt, nil, int64(13), int64(14), int64(15), int64(16), "", createdAt, updatedAt, completedAt,
		))

	repo := &heartbeatProvisioningRepository{db: db}
	result, err := repo.ListLogs(context.Background(), 0, 999)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.Total)
	require.Equal(t, 1, result.Page)
	require.Equal(t, 200, result.PageSize)
	require.Len(t, result.Logs, 1)
	item := result.Logs[0]
	require.Equal(t, int64(9), item.ID)
	require.Equal(t, "complete", item.Status)
	require.Equal(t, int64(13), item.TargetGroupID)
	require.NotNil(t, item.AccountID)
	require.Equal(t, int64(15), *item.AccountID)
	require.NotNil(t, item.CompletedAt)
	require.Equal(t, completedAt, *item.CompletedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHeartbeatProvisioningRepositoryListLogsUnavailable(t *testing.T) {
	repo := &heartbeatProvisioningRepository{}
	_, err := repo.ListLogs(context.Background(), 1, 10)
	require.Error(t, err)
}
