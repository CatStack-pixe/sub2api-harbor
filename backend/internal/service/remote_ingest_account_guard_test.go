//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type remoteIngestOwnershipGuardRepo struct {
	AccountRepository
	remoteIDs []int64
	err       error
	calls     int
}

type remoteProbeEntryPointRepo struct {
	*remoteIngestOwnershipGuardRepo
	getByIDCalls int
}

func (r *remoteIngestOwnershipGuardRepo) ListRemoteIngestAccountIDs(context.Context, []int64) ([]int64, error) {
	r.calls++
	return append([]int64(nil), r.remoteIDs...), r.err
}

func (r *remoteProbeEntryPointRepo) GetByID(context.Context, int64) (*Account, error) {
	r.getByIDCalls++
	return nil, ErrAccountNotFound
}

func TestAdminAccountWritesRejectRemoteIngestAccounts(t *testing.T) {
	repo := &remoteIngestOwnershipGuardRepo{remoteIDs: []int64{42}}
	svc := &adminServiceImpl{accountRepo: repo}
	ctx := context.Background()

	actions := map[string]func() error{
		"duplicate": func() error {
			_, err := svc.DuplicateAccount(ctx, 42, "admin:1", "operation")
			return err
		},
		"update": func() error {
			_, err := svc.UpdateAccount(ctx, 42, &UpdateAccountInput{Name: "changed"})
			return err
		},
		"update extra": func() error {
			return svc.UpdateAccountExtra(ctx, 42, map[string]any{"note": "changed"})
		},
		"bulk update": func() error {
			_, err := svc.BulkUpdateAccounts(ctx, &BulkUpdateAccountsInput{
				AccountIDs: []int64{42},
				Name:       "changed",
				Extra:      map[string]any{},
			})
			return err
		},
		"delete": func() error {
			return svc.DeleteAccount(ctx, 42)
		},
		"clear error": func() error {
			_, err := svc.ClearAccountError(ctx, 42)
			return err
		},
		"set error": func() error {
			return svc.SetAccountError(ctx, 42, "failed")
		},
		"set schedulable": func() error {
			_, err := svc.SetAccountSchedulable(ctx, 42, true)
			return err
		},
		"revert proxy fallback": func() error {
			return svc.RevertAccountProxyFallback(ctx, 42)
		},
		"create shadow": func() error {
			_, err := svc.CreateShadow(ctx, 42, ShadowOptions{})
			return err
		},
	}

	for name, action := range actions {
		t.Run(name, func(t *testing.T) {
			err := action()
			require.ErrorIs(t, err, ErrRemoteIngestAccountManaged)
			require.Equal(t, "REMOTE_INGEST_ACCOUNT_MANAGED", infraerrors.Reason(err))
		})
	}
	require.Equal(t, len(actions), repo.calls)
}

func TestAccountServiceWritesRejectRemoteIngestAccounts(t *testing.T) {
	repo := &remoteIngestOwnershipGuardRepo{remoteIDs: []int64{42}}
	svc := NewAccountService(repo, nil)
	ctx := context.Background()

	_, updateErr := svc.Update(ctx, 42, UpdateAccountRequest{Name: stringPointer("changed")})
	require.ErrorIs(t, updateErr, ErrRemoteIngestAccountManaged)
	require.ErrorIs(t, svc.Delete(ctx, 42), ErrRemoteIngestAccountManaged)
	require.ErrorIs(t, svc.UpdateStatus(ctx, 42, StatusActive, ""), ErrRemoteIngestAccountManaged)
	require.Equal(t, 3, repo.calls)
}

func TestOnlyRemoteIngestProbeEntryPointBypassesManagedAccountGuard(t *testing.T) {
	repo := &remoteProbeEntryPointRepo{
		remoteIngestOwnershipGuardRepo: &remoteIngestOwnershipGuardRepo{remoteIDs: []int64{42}},
	}
	svc := &AccountTestService{accountRepo: repo}

	result, err := svc.RunTestBackground(context.Background(), 42, "test-model")
	require.NoError(t, err)
	require.Equal(t, "failed", result.Status)
	require.Equal(t, 1, repo.calls)
	require.Zero(t, repo.getByIDCalls)

	result, err = svc.RunRemoteIngestProbe(context.Background(), 42, "test-model")
	require.NoError(t, err)
	require.Equal(t, "failed", result.Status)
	require.Equal(t, 1, repo.calls)
	require.Equal(t, 1, repo.getByIDCalls)
}

func TestRemoteIngestOwnershipLookupFailsClosed(t *testing.T) {
	lookupErr := errors.New("database unavailable")
	repo := &remoteIngestOwnershipGuardRepo{err: lookupErr}

	err := rejectRemoteIngestAccountWrite(context.Background(), repo, 42)
	require.ErrorIs(t, err, lookupErr)
	require.NotErrorIs(t, err, ErrRemoteIngestAccountManaged)
}

func TestReservedRemoteIngestMetadataRejected(t *testing.T) {
	for _, key := range []string{
		"remote_ingest",
		"remote_ingest_client_id",
		"remote_ingest.provenance",
		" REMOTE_INGEST_PROVENANCE ",
		"remote_delivery_id",
		"remote_client_id",
		"remote_external_id",
	} {
		t.Run(key, func(t *testing.T) {
			err := rejectReservedRemoteIngestExtra(map[string]any{key: true})
			require.ErrorIs(t, err, ErrRemoteIngestMetadataReserved)
			require.Equal(t, "REMOTE_INGEST_METADATA_RESERVED", infraerrors.Reason(err))
		})
	}
}

func TestCreatePathsRejectReservedRemoteIngestMetadata(t *testing.T) {
	extra := map[string]any{"remote_ingest": true}
	adminSvc := &adminServiceImpl{}
	_, adminErr := adminSvc.CreateAccount(context.Background(), &CreateAccountInput{Extra: extra})
	require.ErrorIs(t, adminErr, ErrRemoteIngestMetadataReserved)

	accountSvc := NewAccountService(nil, nil)
	_, accountErr := accountSvc.Create(context.Background(), CreateAccountRequest{Extra: extra})
	require.ErrorIs(t, accountErr, ErrRemoteIngestMetadataReserved)
}

func stringPointer(value string) *string {
	return &value
}
