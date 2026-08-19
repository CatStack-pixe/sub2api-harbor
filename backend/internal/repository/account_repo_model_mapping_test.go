package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMergeAccountModelMappingUsesAtomicJSONBMerge(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec(`(?s)UPDATE accounts.*\$1::jsonb \|\| CASE.*credentials->'model_mapping'.*INSERT INTO scheduler_outbox`).
		WithArgs(`{"model-a":"model-a"}`, int64(42), service.SchedulerOutboxEventAccountChanged).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := &accountRepository{sql: db}
	err = repo.MergeAccountModelMapping(context.Background(), 42, map[string]string{"model-a": "model-a"})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMergeAccountModelMappingReturnsNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("WITH updated AS").
		WithArgs(`{"model-a":"model-a"}`, int64(404), service.SchedulerOutboxEventAccountChanged).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := &accountRepository{sql: db}
	err = repo.MergeAccountModelMapping(context.Background(), 404, map[string]string{"model-a": "model-a"})
	require.ErrorIs(t, err, service.ErrAccountNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}
