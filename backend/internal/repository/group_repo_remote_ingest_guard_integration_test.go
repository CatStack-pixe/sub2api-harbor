//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestGroupMutationsRejectRemoteIngestBindingsAtomically(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := newGroupRepositoryWithSQL(client, integrationDB)
	suffix := uuid.NewString()

	source, err := client.Group.Create().
		SetName("remote-guard-source-" + suffix).
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	target, err := client.Group.Create().
		SetName("remote-guard-target-" + suffix).
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	account, err := client.Account.Create().
		SetName("remote-guard-account-" + suffix).
		SetPlatform(service.PlatformOpenAI).
		SetType(service.AccountTypeAPIKey).
		SetStatus(service.StatusInactive).
		SetSchedulable(false).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.AccountGroup.Create().
		SetAccountID(account.ID).
		SetGroupID(source.ID).
		SetPriority(11).
		Save(ctx)
	require.NoError(t, err)

	clientID := uuid.NewString()
	deliveryID := uuid.NewString()
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO remote_ingest_clients
			(id, machine_name, public_key, public_key_fingerprint, access_subject)
		VALUES ($1, $2, $3, $4, $5)`,
		clientID, "remote-guard-machine", []byte("public-key"), "fp-"+suffix, "subject-"+suffix)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO remote_ingest_deliveries
			(id, client_id, external_id, payload_hash, query_token_hash,
			 query_token_ciphertext, account_id, platform, group_name, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending')`,
		deliveryID, clientID, "external-"+suffix, []byte("payload-"+suffix), []byte("query-"+suffix),
		"encrypted-query", account.ID, service.PlatformOpenAI, source.Name)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE group_id IN ($1, $2)", source.ID, target.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM remote_ingest_deliveries WHERE id = $1", deliveryID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM remote_ingest_clients WHERE id = $1", clientID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM account_groups WHERE account_id = $1 OR group_id = $2", account.ID, target.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id IN ($1, $2)", source.ID, target.ID)
	})

	sourceModel, err := repo.GetByID(ctx, source.ID)
	require.NoError(t, err)
	sourceModel.Description = "must not persist"
	require.ErrorIs(t, repo.Update(ctx, sourceModel), service.ErrRemoteIngestAccountManaged)

	targetModel, err := repo.GetByID(ctx, target.ID)
	require.NoError(t, err)
	targetModel.Description = "must roll back"
	require.ErrorIs(t, repo.UpdateWithAccountCopy(ctx, targetModel, []int64{source.ID}), service.ErrRemoteIngestAccountManaged)

	createdCopy := &service.Group{
		Name:             "remote-guard-created-copy-" + suffix,
		Platform:         service.PlatformOpenAI,
		RateMultiplier:   1,
		Status:           service.StatusInactive,
		SubscriptionType: service.SubscriptionTypeStandard,
	}
	require.ErrorIs(t, repo.CreateWithAccountCopy(ctx, createdCopy, []int64{source.ID}), service.ErrRemoteIngestAccountManaged)

	duplicate := &service.Group{
		Name:             "remote-guard-duplicate-" + suffix,
		Platform:         service.PlatformOpenAI,
		RateMultiplier:   1,
		Status:           service.StatusInactive,
		SubscriptionType: service.SubscriptionTypeStandard,
	}
	require.ErrorIs(t, repo.CreateFromSource(ctx, duplicate, source.ID), service.ErrRemoteIngestAccountManaged)

	_, err = repo.DeleteCascade(ctx, source.ID)
	require.ErrorIs(t, err, service.ErrRemoteIngestAccountManaged)

	var sourceDescription, targetDescription string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COALESCE(description, '') FROM groups WHERE id = $1", source.ID).Scan(&sourceDescription))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COALESCE(description, '') FROM groups WHERE id = $1", target.ID).Scan(&targetDescription))
	require.Empty(t, sourceDescription)
	require.Empty(t, targetDescription)
	for _, name := range []string{createdCopy.Name, duplicate.Name} {
		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM groups WHERE name = $1", name).Scan(&count))
		require.Zero(t, count)
	}
}

func TestGroupMutationWaitsForRemoteDeliverySharedLock(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := newGroupRepositoryWithSQL(client, integrationDB)
	suffix := uuid.NewString()

	group, err := client.Group.Create().
		SetName("remote-lock-group-" + suffix).
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	account, err := client.Account.Create().
		SetName("remote-lock-account-" + suffix).
		SetPlatform(service.PlatformOpenAI).
		SetType(service.AccountTypeAPIKey).
		SetStatus(service.StatusInactive).
		SetSchedulable(false).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.AccountGroup.Create().SetAccountID(account.ID).SetGroupID(group.ID).Save(ctx)
	require.NoError(t, err)
	clientID := uuid.NewString()
	deliveryID := uuid.NewString()
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO remote_ingest_clients
			(id, machine_name, public_key, public_key_fingerprint, access_subject)
		VALUES ($1, $2, $3, $4, $5)`,
		clientID, "remote-lock-machine", []byte("public-key"), "fp-"+suffix, "subject-"+suffix)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE group_id = $1", group.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM remote_ingest_deliveries WHERE id = $1", deliveryID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM remote_ingest_clients WHERE id = $1", clientID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM account_groups WHERE account_id = $1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id = $1", group.ID)
	})

	deliveryTx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = deliveryTx.Rollback() }()
	var lockedID int64
	require.NoError(t, deliveryTx.QueryRowContext(ctx, "SELECT id FROM groups WHERE id = $1 FOR SHARE", group.ID).Scan(&lockedID))
	require.Equal(t, group.ID, lockedID)
	_, err = deliveryTx.ExecContext(ctx, `
		INSERT INTO remote_ingest_deliveries
			(id, client_id, external_id, payload_hash, query_token_hash,
			 query_token_ciphertext, account_id, platform, group_name, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending')`,
		deliveryID, clientID, "external-"+suffix, []byte("payload-"+suffix), []byte("query-"+suffix),
		"encrypted-query", account.ID, service.PlatformOpenAI, group.Name)
	require.NoError(t, err)

	groupModel, err := repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	groupModel.Description = "must not persist"
	result := make(chan error, 1)
	go func() {
		result <- repo.Update(context.Background(), groupModel)
	}()

	select {
	case err := <-result:
		require.Failf(t, "group update returned before delivery commit", "error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, deliveryTx.Commit())

	select {
	case err := <-result:
		require.ErrorIs(t, err, service.ErrRemoteIngestAccountManaged)
	case <-time.After(5 * time.Second):
		require.Fail(t, "group update stayed blocked after delivery commit")
	}
}
