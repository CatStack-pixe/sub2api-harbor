package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type remoteIngestRepository struct {
	db *sql.DB
}

func NewRemoteIngestRepository(db *sql.DB) service.RemoteIngestRepository {
	return &remoteIngestRepository{db: db}
}

func (r *remoteIngestRepository) CreateRegistrationToken(ctx context.Context, token *service.RemoteRegistrationToken, tokenHash []byte) error {
	if token == nil { return errors.New("nil remote registration token") }
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO remote_ingest_registration_tokens
			(id, token_hash, token_fingerprint, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		token.ID, tokenHash, token.Fingerprint, token.ExpiresAt, token.CreatedAt)
	return err
}

func (r *remoteIngestRepository) ListRegistrationTokens(ctx context.Context, limit int) ([]service.RemoteRegistrationToken, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, token_fingerprint, expires_at, used_at,
		       used_by_client_id::text, created_at
		FROM remote_ingest_registration_tokens
		ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil { return nil, err }
	defer func() { _ = rows.Close() }()
	items := make([]service.RemoteRegistrationToken, 0)
	for rows.Next() {
		var item service.RemoteRegistrationToken
		var usedAt sql.NullTime
		var clientID sql.NullString
		if err := rows.Scan(&item.ID, &item.Fingerprint, &item.ExpiresAt, &usedAt, &clientID, &item.CreatedAt); err != nil { return nil, err }
		if usedAt.Valid { item.UsedAt = &usedAt.Time }
		if clientID.Valid { value := clientID.String; item.ClientID = &value }
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *remoteIngestRepository) ConsumeRegistrationToken(ctx context.Context, tokenHash []byte, client *service.RemoteClient) error {
	if client == nil { return service.ErrRemoteTokenInvalid }
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil { return err }
	defer func() { _ = tx.Rollback() }()

	var tokenID string
	err = tx.QueryRowContext(ctx, `
		UPDATE remote_ingest_registration_tokens
		SET used_at = NOW()
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > NOW()
		RETURNING id::text`, tokenHash).Scan(&tokenID)
	if errors.Is(err, sql.ErrNoRows) { return service.ErrRemoteTokenInvalid }
	if err != nil { return err }
	_, err = tx.ExecContext(ctx, `
		INSERT INTO remote_ingest_clients
			(id, machine_name, public_key, public_key_fingerprint, access_subject, enrolled_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		client.ID, client.MachineName, client.PublicKey, client.PublicKeyFingerprint, client.AccessSubject, client.EnrolledAt)
	if err != nil { return translateRemoteConstraint(err) }
	if _, err := tx.ExecContext(ctx, `
		UPDATE remote_ingest_registration_tokens SET used_by_client_id = $1 WHERE id = $2`,
		client.ID, tokenID); err != nil { return err }
	return tx.Commit()
}

func (r *remoteIngestRepository) GetClient(ctx context.Context, id string) (*service.RemoteClient, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, machine_name, public_key, public_key_fingerprint,
		       access_subject, enrolled_at, last_active_at, revoked_at
		FROM remote_ingest_clients WHERE id = $1`, id)
	item, err := scanRemoteClient(row)
	if errors.Is(err, sql.ErrNoRows) { return nil, service.ErrRemoteClientUnauthorized }
	return item, err
}

func (r *remoteIngestRepository) ListClients(ctx context.Context, limit int) ([]service.RemoteClient, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, machine_name, public_key, public_key_fingerprint,
		       access_subject, enrolled_at, last_active_at, revoked_at
		FROM remote_ingest_clients ORDER BY enrolled_at DESC LIMIT $1`, limit)
	if err != nil { return nil, err }
	defer func() { _ = rows.Close() }()
	items := make([]service.RemoteClient, 0)
	for rows.Next() {
		item, err := scanRemoteClient(rows)
		if err != nil { return nil, err }
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *remoteIngestRepository) RevokeClient(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil { return err }
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE remote_ingest_clients SET revoked_at = COALESCE(revoked_at, NOW()) WHERE id = $1`, id)
	if err != nil { return err }
	if count, _ := result.RowsAffected(); count == 0 { return service.ErrRemoteClientUnauthorized }
	if _, err := tx.ExecContext(ctx, `
		UPDATE accounts a
		SET status = 'inactive', schedulable = FALSE,
		    error_message = 'remote ingest client revoked', updated_at = NOW()
		WHERE a.deleted_at IS NULL AND EXISTS (
			SELECT 1 FROM remote_ingest_deliveries d
			WHERE d.client_id = $1 AND d.account_id = a.id
			  AND d.status IN ('pending', 'probing')
		)`, id); err != nil { return err }
	if _, err := tx.ExecContext(ctx, `
		UPDATE remote_ingest_deliveries
		SET status = 'probe_failed', masked_error = 'remote ingest client revoked',
		    lease_until = NULL, completed_at = NOW(), updated_at = NOW()
		WHERE client_id = $1 AND status IN ('pending', 'probing')`, id); err != nil { return err }
	return tx.Commit()
}

func (r *remoteIngestRepository) TouchClient(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE remote_ingest_clients SET last_active_at = NOW() WHERE id = $1 AND revoked_at IS NULL`, id)
	return err
}

func (r *remoteIngestRepository) CreateDelivery(ctx context.Context, create service.RemoteDeliveryCreate) (*service.RemoteDelivery, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil { return nil, false, err }
	defer func() { _ = tx.Rollback() }()

	existing, existingHash, err := getRemoteDeliveryByExternalID(ctx, tx, create.ClientID, create.Submission.ExternalID)
	if err == nil {
		if !bytes.Equal(existingHash, create.PayloadHash) { return nil, false, service.ErrRemoteDeliveryConflict }
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) { return nil, false, err }

	var revokedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT revoked_at FROM remote_ingest_clients WHERE id = $1 FOR SHARE`, create.ClientID).Scan(&revokedAt); err != nil {
		return nil, false, service.ErrRemoteClientUnauthorized
	}
	if revokedAt.Valid { return nil, false, service.ErrRemoteClientRevoked }

	rows, err := tx.QueryContext(ctx, `
		SELECT id, platform, require_oauth_only
		FROM groups
		WHERE name = $1 AND status = 'active' AND deleted_at IS NULL
		FOR SHARE`, create.Submission.GroupName)
	if err != nil { return nil, false, err }
	var groupID int64
	var groupPlatform string
	var requireOAuthOnly bool
	count := 0
	for rows.Next() {
		count++
		if err := rows.Scan(&groupID, &groupPlatform, &requireOAuthOnly); err != nil {
			_ = rows.Close()
			return nil, false, err
		}
	}
	if err := rows.Close(); err != nil {
		return nil, false, err
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if count != 1 || groupPlatform != create.Submission.Platform || requireOAuthOnly {
		return nil, false, service.ErrRemoteGroupInvalid
	}

	credentials, err := json.Marshal(map[string]any{
		"api_key": create.EncryptedAPIKey,
		"base_url": create.Submission.BaseURL,
	})
	if err != nil { return nil, false, err }
	extra, err := json.Marshal(map[string]any{
		"remote_ingest": true,
		"remote_delivery_id": create.ID,
		"remote_client_id": create.ClientID,
		"remote_external_id": create.Submission.ExternalID,
	})
	if err != nil { return nil, false, err }
	var accountID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO accounts
			(name, platform, type, credentials, extra, concurrency, priority,
			 rate_multiplier, status, schedulable)
		VALUES ($1, $2, 'apikey', $3::jsonb, $4::jsonb, $5, $6, $7, 'inactive', FALSE)
		RETURNING id`, create.Submission.Name, create.Submission.Platform, string(credentials), string(extra),
		create.Submission.Concurrency, create.Submission.Priority, create.Submission.RateMultiplier).Scan(&accountID)
	if err != nil { return nil, false, err }
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_groups (account_id, group_id, priority)
		VALUES ($1, $2, $3)`, accountID, groupID, create.Submission.Priority); err != nil { return nil, false, err }

	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO remote_ingest_deliveries
			(id, client_id, external_id, payload_hash, query_token_hash,
			 query_token_ciphertext, account_id, platform, group_name, test_model,
			 status, next_attempt_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		        'pending', NOW(), $11, $11)`,
		create.ID, create.ClientID, create.Submission.ExternalID, create.PayloadHash,
		create.QueryTokenHash, create.QueryTokenCiphertext, accountID, create.Submission.Platform,
		create.Submission.GroupName, nullIfEmpty(create.Submission.TestModel), now)
	if err != nil {
		if isRemoteExternalIDConflict(err) {
			_ = tx.Rollback()
			return r.resolveConcurrentDelivery(ctx, create)
		}
		return nil, false, translateRemoteConstraint(err)
	}
	if err := enqueueRemoteSchedulerOutbox(ctx, tx, accountID, groupID); err != nil { return nil, false, err }
	if err := tx.Commit(); err != nil { return nil, false, err }
	service.RegisterRemoteIngestAccount(accountID)
	return &service.RemoteDelivery{
		ID: create.ID, ClientID: create.ClientID, ExternalID: create.Submission.ExternalID,
		AccountID: accountID, Platform: create.Submission.Platform, GroupName: create.Submission.GroupName,
		TestModel: create.Submission.TestModel, Status: service.RemoteDeliveryPending,
		CreatedAt: now, UpdatedAt: now, QueryCipher: create.QueryTokenCiphertext,
	}, true, nil
}

func (r *remoteIngestRepository) GetDelivery(ctx context.Context, id string, queryTokenHash []byte) (*service.RemoteDelivery, error) {
	row := r.db.QueryRowContext(ctx, remoteDeliverySelect+` WHERE d.id = $1 AND d.query_token_hash = $2`, id, queryTokenHash)
	item, _, err := scanRemoteDelivery(row, false)
	if errors.Is(err, sql.ErrNoRows) { return nil, service.ErrRemoteDeliveryNotFound }
	return item, err
}

func (r *remoteIngestRepository) ListDeliveries(ctx context.Context, status string, limit int) ([]service.RemoteDelivery, error) {
	query := remoteDeliverySelect
	args := []any{}
	if status != "" { query += ` WHERE d.status = $1`; args = append(args, status) }
	args = append(args, limit)
	query += fmt.Sprintf(` ORDER BY d.created_at DESC LIMIT $%d`, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil { return nil, err }
	defer rows.Close()
	items := make([]service.RemoteDelivery, 0)
	for rows.Next() {
		item, _, err := scanRemoteDelivery(rows, false)
		if err != nil { return nil, err }
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *remoteIngestRepository) ClaimProbe(ctx context.Context, lease time.Duration) (*service.RemoteProbeJob, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil { return nil, err }
	defer func() { _ = tx.Rollback() }()
	row := tx.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT d.id FROM remote_ingest_deliveries d
			JOIN remote_ingest_clients c ON c.id = d.client_id AND c.revoked_at IS NULL
			WHERE (d.status = 'pending' AND d.next_attempt_at <= NOW())
			   OR (d.status = 'probing' AND d.lease_until < NOW())
			ORDER BY d.created_at
			FOR UPDATE SKIP LOCKED LIMIT 1
		)
		UPDATE remote_ingest_deliveries d
		SET status = 'probing', attempts = attempts + 1,
		    lease_until = NOW() + ($1 * INTERVAL '1 second'), updated_at = NOW()
		FROM candidate c WHERE d.id = c.id
		RETURNING d.id::text, d.account_id, COALESCE(d.test_model, ''), d.attempts`, int64(lease/time.Second))
	var job service.RemoteProbeJob
	if err := row.Scan(&job.DeliveryID, &job.AccountID, &job.TestModel, &job.Attempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) { return nil, tx.Commit() }
		return nil, err
	}
	if err := tx.Commit(); err != nil { return nil, err }
	return &job, nil
}

func (r *remoteIngestRepository) CompleteProbe(ctx context.Context, deliveryID string, attempt int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil { return err }
	defer func() { _ = tx.Rollback() }()
	var accountID, groupID int64
	err = tx.QueryRowContext(ctx, `
		SELECT d.account_id, ag.group_id
		FROM remote_ingest_deliveries d
		JOIN remote_ingest_clients c ON c.id = d.client_id
		JOIN accounts a ON a.id = d.account_id
		JOIN account_groups ag ON ag.account_id = d.account_id
		JOIN groups g ON g.id = ag.group_id
		WHERE d.id = $1 AND d.status = 'probing' AND d.attempts = $2
		  AND c.revoked_at IS NULL
		  AND a.deleted_at IS NULL AND a.status = 'inactive' AND a.schedulable = FALSE
		  AND g.deleted_at IS NULL AND g.status = 'active'
		  AND g.name = d.group_name AND g.platform = d.platform
		  AND g.require_oauth_only = FALSE
		FOR UPDATE OF d, c, a, g`, deliveryID, attempt).Scan(&accountID, &groupID)
	if errors.Is(err, sql.ErrNoRows) { return service.ErrRemoteDeliveryNotFound }
	if err != nil { return err }
	if _, err := tx.ExecContext(ctx, `
		UPDATE accounts SET status = 'active', schedulable = TRUE,
		       error_message = NULL, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, accountID); err != nil { return err }
	if _, err := tx.ExecContext(ctx, `
		UPDATE remote_ingest_deliveries
		SET status = 'active', masked_error = NULL, lease_until = NULL,
		    completed_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status = 'probing' AND attempts = $2`, deliveryID, attempt); err != nil { return err }
	if err := enqueueRemoteSchedulerOutbox(ctx, tx, accountID, groupID); err != nil { return err }
	return tx.Commit()
}

func (r *remoteIngestRepository) FailProbe(ctx context.Context, deliveryID string, attempt int, maskedError string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil { return err }
	defer func() { _ = tx.Rollback() }()
	var accountID int64
	err = tx.QueryRowContext(ctx, `
		UPDATE remote_ingest_deliveries
		SET status = 'probe_failed', masked_error = $2, lease_until = NULL,
		    completed_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status = 'probing' AND attempts = $3
		RETURNING account_id`, deliveryID, maskedError, attempt).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) { return service.ErrRemoteDeliveryNotFound }
	if err != nil { return err }
	if _, err := tx.ExecContext(ctx, `
		UPDATE accounts SET status = 'inactive', schedulable = FALSE,
		       error_message = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, accountID, maskedError); err != nil { return err }
	return tx.Commit()
}

func (r *remoteIngestRepository) RetryProbe(ctx context.Context, deliveryID string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE remote_ingest_deliveries
		SET status = 'pending', masked_error = NULL, next_attempt_at = NOW(),
		    lease_until = NULL, completed_at = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'probe_failed'
		  AND EXISTS (
			SELECT 1 FROM remote_ingest_clients c
			WHERE c.id = remote_ingest_deliveries.client_id AND c.revoked_at IS NULL
		  )`, deliveryID)
	if err != nil { return err }
	if count, _ := result.RowsAffected(); count == 0 { return service.ErrRemoteDeliveryNotFound }
	return nil
}

const remoteDeliverySelect = `
	SELECT d.id::text, d.client_id::text, d.external_id, d.payload_hash,
	       d.query_token_ciphertext, d.account_id, d.platform, d.group_name,
	       COALESCE(d.test_model, ''), d.status, COALESCE(d.masked_error, ''),
	       d.attempts, d.created_at, d.updated_at, d.completed_at
	FROM remote_ingest_deliveries d`

type remoteScanner interface { Scan(dest ...any) error }

func scanRemoteClient(scanner remoteScanner) (*service.RemoteClient, error) {
	var item service.RemoteClient
	var lastActive, revoked sql.NullTime
	err := scanner.Scan(&item.ID, &item.MachineName, &item.PublicKey, &item.PublicKeyFingerprint,
		&item.AccessSubject, &item.EnrolledAt, &lastActive, &revoked)
	if err != nil { return nil, err }
	if lastActive.Valid { item.LastActiveAt = &lastActive.Time }
	if revoked.Valid { item.RevokedAt = &revoked.Time }
	return &item, nil
}

func scanRemoteDelivery(scanner remoteScanner, withHash bool) (*service.RemoteDelivery, []byte, error) {
	var item service.RemoteDelivery
	var payloadHash []byte
	var completed sql.NullTime
	err := scanner.Scan(&item.ID, &item.ClientID, &item.ExternalID, &payloadHash, &item.QueryCipher,
		&item.AccountID, &item.Platform, &item.GroupName, &item.TestModel, &item.Status,
		&item.MaskedError, &item.Attempts, &item.CreatedAt, &item.UpdatedAt, &completed)
	if err != nil { return nil, nil, err }
	if completed.Valid { item.CompletedAt = &completed.Time }
	return &item, payloadHash, nil
}

func getRemoteDeliveryByExternalID(ctx context.Context, tx *sql.Tx, clientID, externalID string) (*service.RemoteDelivery, []byte, error) {
	return scanRemoteDelivery(tx.QueryRowContext(ctx, remoteDeliverySelect+`
		WHERE d.client_id = $1 AND d.external_id = $2 FOR UPDATE`, clientID, externalID), true)
}

func enqueueRemoteSchedulerOutbox(ctx context.Context, tx *sql.Tx, accountID, groupID int64) error {
	return enqueueSchedulerOutbox(
		ctx,
		tx,
		service.SchedulerOutboxEventAccountChanged,
		&accountID,
		nil,
		buildSchedulerGroupPayload([]int64{groupID}),
	)
}

func nullIfEmpty(value string) any {
	if value == "" { return nil }
	return value
}

func translateRemoteConstraint(err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		switch pqErr.Constraint {
		case "remote_ingest_clients_public_key_fingerprint_key", "remote_ingest_clients_access_subject_key":
			return service.ErrRemoteClientConflict
		}
		return service.ErrRemoteDeliveryConflict
	}
	return err
}

func isRemoteExternalIDConflict(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505" &&
		pqErr.Constraint == "remote_ingest_deliveries_client_external_unique"
}

func (r *remoteIngestRepository) resolveConcurrentDelivery(ctx context.Context, create service.RemoteDeliveryCreate) (*service.RemoteDelivery, bool, error) {
	row := r.db.QueryRowContext(ctx, remoteDeliverySelect+`
		WHERE d.client_id = $1 AND d.external_id = $2`, create.ClientID, create.Submission.ExternalID)
	existing, payloadHash, err := scanRemoteDelivery(row, true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, service.ErrRemoteDeliveryConflict
	}
	if err != nil {
		return nil, false, err
	}
	if !bytes.Equal(payloadHash, create.PayloadHash) {
		return nil, false, service.ErrRemoteDeliveryConflict
	}
	return existing, false, nil
}
