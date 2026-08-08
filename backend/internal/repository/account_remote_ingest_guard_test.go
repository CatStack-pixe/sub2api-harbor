//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func TestListRemoteIngestAccountIDsUsesDeliveryRelationship(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT DISTINCT account_id\s+FROM remote_ingest_deliveries\s+WHERE account_id = ANY\(\$1\)`).
		WithArgs(pq.Array([]int64{11, 22, 33})).
		WillReturnRows(sqlmock.NewRows([]string{"account_id"}).AddRow(int64(22)))

	repo := &accountRepository{sql: db}
	remoteIDs, err := repo.ListRemoteIngestAccountIDs(context.Background(), []int64{11, 22, 33})
	require.NoError(t, err)
	require.Equal(t, []int64{22}, remoteIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateCredentialsExcludesRemoteIngestAccountsAtRepositoryBoundary(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE accounts.*NOT EXISTS.*remote_ingest_deliveries`).
		WithArgs(`{"api_key":"plaintext-must-not-be-written"}`, int64(22)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	err = repo.UpdateCredentials(context.Background(), 22, map[string]any{"api_key": "plaintext-must-not-be-written"})
	require.ErrorIs(t, err, service.ErrAccountNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}
