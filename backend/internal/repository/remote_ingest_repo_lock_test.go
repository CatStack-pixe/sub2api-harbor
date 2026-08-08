package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestRemoteIngestRevokeClientLocksDeliveriesBeforeClient(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	repository := &remoteIngestRepository{db: db}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id::text FROM remote_ingest_deliveries.*FOR UPDATE`).
		WithArgs("client-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("delivery-1"))
	mock.ExpectExec(`UPDATE remote_ingest_clients SET revoked_at`).
		WithArgs("client-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE accounts a`).
		WithArgs("client-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE remote_ingest_deliveries`).
		WithArgs("client-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repository.RevokeClient(context.Background(), "client-1"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRemoteIngestRetryProbeLocksDeliveryThenActiveClient(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	repository := &remoteIngestRepository{db: db}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT client_id::text FROM remote_ingest_deliveries.*FOR UPDATE`).
		WithArgs("delivery-1").
		WillReturnRows(sqlmock.NewRows([]string{"client_id"}).AddRow("client-1"))
	mock.ExpectQuery(`SELECT id::text FROM remote_ingest_clients.*FOR UPDATE`).
		WithArgs("client-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("client-1"))
	mock.ExpectExec(`UPDATE remote_ingest_deliveries`).
		WithArgs("delivery-1", "client-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repository.RetryProbe(context.Background(), "delivery-1"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRemoteIngestRetryProbeRejectsRevokedClientInsideTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	repository := &remoteIngestRepository{db: db}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT client_id::text FROM remote_ingest_deliveries.*FOR UPDATE`).
		WithArgs("delivery-1").
		WillReturnRows(sqlmock.NewRows([]string{"client_id"}).AddRow("client-1"))
	mock.ExpectQuery(`SELECT id::text FROM remote_ingest_clients.*FOR UPDATE`).
		WithArgs("client-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	err = repository.RetryProbe(context.Background(), "delivery-1")
	require.ErrorIs(t, err, service.ErrRemoteDeliveryNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}
