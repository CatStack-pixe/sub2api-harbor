package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type heartbeatProvisioningRepository struct {
	db *sql.DB
}

func NewHeartbeatProvisioningRepository(db *sql.DB) service.HeartbeatProvisioningRepository {
	return &heartbeatProvisioningRepository{db: db}
}

func (r *heartbeatProvisioningRepository) Enqueue(ctx context.Context, input service.HeartbeatProvisioningEnqueueInput) error {
	if r == nil || r.db == nil {
		return errors.New("nil heartbeat provisioning database")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO heartbeat_provision_jobs (provider, fingerprint, session_key_ciphertext, source_balance, source_checked_at, target_group_id, target_proxy_group_id)
		VALUES ($1, $2, $3, $4, NULLIF($5::timestamptz, 'epoch'::timestamptz), $6, NULLIF($7::bigint, 0))
		ON CONFLICT (provider, fingerprint) DO UPDATE SET
			session_key_ciphertext = EXCLUDED.session_key_ciphertext,
			source_balance = EXCLUDED.source_balance,
			source_checked_at = COALESCE(EXCLUDED.source_checked_at, heartbeat_provision_jobs.source_checked_at),
			target_group_id = EXCLUDED.target_group_id,
			target_proxy_group_id = EXCLUDED.target_proxy_group_id,
			status = CASE
				WHEN heartbeat_provision_jobs.status <> 'processing'
					AND (heartbeat_provision_jobs.status = 'failed'
					OR heartbeat_provision_jobs.target_group_id IS DISTINCT FROM EXCLUDED.target_group_id
					OR heartbeat_provision_jobs.target_proxy_group_id IS DISTINCT FROM EXCLUDED.target_proxy_group_id)
				THEN 'queued'
				ELSE heartbeat_provision_jobs.status
			END,
			attempts = CASE
				WHEN heartbeat_provision_jobs.status <> 'processing'
					AND (heartbeat_provision_jobs.status = 'failed'
					OR heartbeat_provision_jobs.target_group_id IS DISTINCT FROM EXCLUDED.target_group_id
					OR heartbeat_provision_jobs.target_proxy_group_id IS DISTINCT FROM EXCLUDED.target_proxy_group_id)
				THEN 0
				ELSE heartbeat_provision_jobs.attempts
			END,
			available_at = CASE
				WHEN heartbeat_provision_jobs.status <> 'processing'
					AND (heartbeat_provision_jobs.status = 'failed'
					OR heartbeat_provision_jobs.target_group_id IS DISTINCT FROM EXCLUDED.target_group_id
					OR heartbeat_provision_jobs.target_proxy_group_id IS DISTINCT FROM EXCLUDED.target_proxy_group_id)
				THEN NOW()
				ELSE heartbeat_provision_jobs.available_at
			END,
			locked_until = CASE
				WHEN heartbeat_provision_jobs.status <> 'processing'
					AND (heartbeat_provision_jobs.status = 'failed'
					OR heartbeat_provision_jobs.target_group_id IS DISTINCT FROM EXCLUDED.target_group_id
					OR heartbeat_provision_jobs.target_proxy_group_id IS DISTINCT FROM EXCLUDED.target_proxy_group_id)
				THEN NULL
				ELSE heartbeat_provision_jobs.locked_until
			END,
			lock_owner = CASE
				WHEN heartbeat_provision_jobs.status <> 'processing'
					AND (heartbeat_provision_jobs.status = 'failed'
					OR heartbeat_provision_jobs.target_group_id IS DISTINCT FROM EXCLUDED.target_group_id
					OR heartbeat_provision_jobs.target_proxy_group_id IS DISTINCT FROM EXCLUDED.target_proxy_group_id)
				THEN NULL
				ELSE heartbeat_provision_jobs.lock_owner
			END,
			last_error = CASE
				WHEN heartbeat_provision_jobs.status <> 'processing'
					AND (heartbeat_provision_jobs.status = 'failed'
					OR heartbeat_provision_jobs.target_group_id IS DISTINCT FROM EXCLUDED.target_group_id
					OR heartbeat_provision_jobs.target_proxy_group_id IS DISTINCT FROM EXCLUDED.target_proxy_group_id)
				THEN NULL
				ELSE heartbeat_provision_jobs.last_error
			END,
			completed_at = CASE
				WHEN heartbeat_provision_jobs.status <> 'processing'
					AND (heartbeat_provision_jobs.status = 'failed'
					OR heartbeat_provision_jobs.target_group_id IS DISTINCT FROM EXCLUDED.target_group_id
					OR heartbeat_provision_jobs.target_proxy_group_id IS DISTINCT FROM EXCLUDED.target_proxy_group_id)
				THEN NULL
				ELSE heartbeat_provision_jobs.completed_at
			END,
			updated_at = NOW()
	`, input.Provider, input.Fingerprint, input.SessionKeyCiphertext, input.SourceBalance, input.SourceCheckedAt, input.TargetGroupID, input.TargetProxyGroupID)
	return err
}

func (r *heartbeatProvisioningRepository) Claim(ctx context.Context, workerID string, lease time.Duration) (*service.HeartbeatProvisioningJob, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("nil heartbeat provisioning database")
	}
	seconds := int64(lease / time.Second)
	if seconds < 1 {
		seconds = 60
	}
	row := r.db.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT id
			FROM heartbeat_provision_jobs
			WHERE (status IN ('queued', 'retry') AND available_at <= NOW())
			   OR (status = 'processing' AND locked_until < NOW())
			ORDER BY available_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE heartbeat_provision_jobs AS job
		SET status = 'processing', attempts = attempts + 1, lock_owner = $1,
			locked_until = NOW() + ($2 * INTERVAL '1 second'), updated_at = NOW()
		FROM candidate
		WHERE job.id = candidate.id
		RETURNING job.id, job.provider, job.fingerprint, job.session_key_ciphertext, job.attempts,
			job.target_group_id, job.target_proxy_group_id, job.account_id, job.proxy_id
	`, workerID, seconds)
	job := &service.HeartbeatProvisioningJob{}
	var targetGroupID, targetProxyGroupID, accountID, proxyID sql.NullInt64
	if err := row.Scan(&job.ID, &job.Provider, &job.Fingerprint, &job.SessionKeyCiphertext, &job.Attempts, &targetGroupID, &targetProxyGroupID, &accountID, &proxyID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if targetGroupID.Valid {
		job.TargetGroupID = targetGroupID.Int64
	}
	if targetProxyGroupID.Valid {
		job.TargetProxyGroupID = targetProxyGroupID.Int64
	}
	if accountID.Valid {
		value := accountID.Int64
		job.AccountID = &value
	}
	if proxyID.Valid {
		value := proxyID.Int64
		job.ProxyID = &value
	}
	return job, nil
}

func (r *heartbeatProvisioningRepository) Stats(ctx context.Context) (*service.HeartbeatQueueStats, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("nil heartbeat provisioning database")
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'queued'),
			COUNT(*) FILTER (WHERE status = 'processing'),
			COUNT(*) FILTER (WHERE status = 'retry'),
			COUNT(*) FILTER (WHERE status = 'failed'),
			COUNT(*) FILTER (WHERE status = 'complete'),
			COALESCE((SELECT last_error FROM heartbeat_provision_jobs WHERE last_error IS NOT NULL AND last_error <> '' ORDER BY updated_at DESC LIMIT 1), ''),
			(SELECT updated_at FROM heartbeat_provision_jobs WHERE last_error IS NOT NULL AND last_error <> '' ORDER BY updated_at DESC LIMIT 1)
		FROM heartbeat_provision_jobs
	`)
	stats := &service.HeartbeatQueueStats{}
	var lastErrorAt sql.NullTime
	if err := row.Scan(&stats.Queued, &stats.Processing, &stats.Retry, &stats.Failed, &stats.Complete, &stats.LastError, &lastErrorAt); err != nil {
		return nil, err
	}
	if lastErrorAt.Valid {
		value := lastErrorAt.Time
		stats.LastErrorAt = &value
	}
	return stats, nil
}

func (r *heartbeatProvisioningRepository) ListLogs(ctx context.Context, page, pageSize int) (*service.HeartbeatProvisioningLogList, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("nil heartbeat provisioning database")
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM heartbeat_provision_jobs`).Scan(&total); err != nil {
		return nil, err
	}
	offset := (page - 1) * pageSize
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, provider, fingerprint, source_balance, source_checked_at, status,
		       attempts, available_at, locked_until, target_group_id,
		       target_proxy_group_id, account_id, proxy_id, last_error,
		       created_at, updated_at, completed_at
		FROM heartbeat_provision_jobs
		ORDER BY updated_at DESC, id DESC
		LIMIT $1 OFFSET $2
	`, pageSize, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	logs := make([]*service.HeartbeatProvisioningLog, 0, pageSize)
	for rows.Next() {
		item := &service.HeartbeatProvisioningLog{}
		var sourceBalance sql.NullFloat64
		var sourceCheckedAt, lockedUntil, completedAt sql.NullTime
		var targetGroupID, targetProxyGroupID, accountID, proxyID sql.NullInt64
		var lastError sql.NullString
		if err := rows.Scan(
			&item.ID, &item.Provider, &item.Fingerprint, &sourceBalance, &sourceCheckedAt,
			&item.Status, &item.Attempts, &item.AvailableAt, &lockedUntil,
			&targetGroupID, &targetProxyGroupID, &accountID, &proxyID,
			&lastError, &item.CreatedAt, &item.UpdatedAt, &completedAt,
		); err != nil {
			return nil, err
		}
		if sourceBalance.Valid {
			value := sourceBalance.Float64
			item.SourceBalance = &value
		}
		if targetGroupID.Valid {
			item.TargetGroupID = targetGroupID.Int64
		}
		if sourceCheckedAt.Valid {
			value := sourceCheckedAt.Time
			item.SourceCheckedAt = &value
		}
		if lockedUntil.Valid {
			value := lockedUntil.Time
			item.LockedUntil = &value
		}
		if targetProxyGroupID.Valid {
			value := targetProxyGroupID.Int64
			item.TargetProxyGroupID = &value
		}
		if accountID.Valid {
			value := accountID.Int64
			item.AccountID = &value
		}
		if proxyID.Valid {
			value := proxyID.Int64
			item.ProxyID = &value
		}
		if lastError.Valid {
			item.LastError = lastError.String
		}
		if completedAt.Valid {
			value := completedAt.Time
			item.CompletedAt = &value
		}
		logs = append(logs, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &service.HeartbeatProvisioningLogList{Logs: logs, Total: total, Page: page, PageSize: pageSize}, nil
}

func (r *heartbeatProvisioningRepository) SetProxy(ctx context.Context, jobID, proxyID int64) error {
	return r.updateClaimed(ctx, `proxy_id = $2, updated_at = NOW()`, jobID, proxyID)
}

func (r *heartbeatProvisioningRepository) SetAccount(ctx context.Context, jobID, accountID int64) error {
	return r.updateClaimed(ctx, `account_id = $2, updated_at = NOW()`, jobID, accountID)
}

func (r *heartbeatProvisioningRepository) FindPendingAccountByFingerprint(ctx context.Context, fingerprint string) (*int64, error) {
	return r.FindPendingAccountByProviderAndFingerprint(ctx, "ds", fingerprint)
}

func (r *heartbeatProvisioningRepository) FindPendingAccountByProviderAndFingerprint(ctx context.Context, provider, fingerprint string) (*int64, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("nil heartbeat provisioning database")
	}
	providerID, ok := service.HeartbeatProviderID(provider)
	if !ok {
		return nil, fmt.Errorf("unsupported heartbeat provider %q", provider)
	}
	platform, _ := service.HeartbeatProviderPlatform(providerID)
	var id int64
	err := r.db.QueryRowContext(ctx, `
		SELECT id
		FROM accounts
		WHERE platform = $1
		  AND status = 'disabled'
		  AND schedulable = FALSE
		  AND deleted_at IS NULL
		  AND extra ->> 'heartbeat_fp' = $2
		  AND (extra ->> 'heartbeat_provider' = $3
		    OR ($3 = 'ds' AND NULLIF(extra ->> 'heartbeat_provider', '') IS NULL))
		ORDER BY id DESC
		LIMIT 1
	`, platform, fingerprint, strings.ToLower(strings.TrimSpace(providerID))).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (r *heartbeatProvisioningRepository) updateClaimed(ctx context.Context, setClause string, jobID, value int64) error {
	if r == nil || r.db == nil {
		return errors.New("nil heartbeat provisioning database")
	}
	query := fmt.Sprintf(`UPDATE heartbeat_provision_jobs SET %s WHERE id = $1 AND status = 'processing'`, setClause)
	result, err := r.db.ExecContext(ctx, query, jobID, value)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("heartbeat provisioning job %d is no longer claimed", jobID)
	}
	return nil
}

func (r *heartbeatProvisioningRepository) Complete(ctx context.Context, jobID int64) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE heartbeat_provision_jobs
		SET status = 'complete', locked_until = NULL, lock_owner = NULL, last_error = NULL, completed_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status = 'processing'
	`, jobID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("heartbeat provisioning job %d cannot complete", jobID)
	}
	return nil
}

func (r *heartbeatProvisioningRepository) Retry(ctx context.Context, jobID int64, attempts int, availableAt time.Time, terminal bool, lastError string) error {
	status := "retry"
	if terminal {
		status = "failed"
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE heartbeat_provision_jobs
		SET status = $2, available_at = $3, locked_until = NULL, lock_owner = NULL, last_error = $4, updated_at = NOW()
		WHERE id = $1 AND status = 'processing'
	`, jobID, status, availableAt, lastError)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("heartbeat provisioning job %d cannot retry after attempt %d", jobID, attempts)
	}
	return nil
}
