package repository

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type emailDeliveryRepository struct{ client *ent.Client }

func NewEmailDeliveryRepository(client *ent.Client) service.EmailDeliveryRepository {
	return &emailDeliveryRepository{client: client}
}

func (r *emailDeliveryRepository) RecordResendEvent(ctx context.Context, event service.ResendDeliveryEvent) (bool, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO email_delivery_events
			(provider, event_id, provider_email_id, event_type, event_created_at)
		VALUES ('resend', $1, $2, $3, $4)
		ON CONFLICT (provider, event_id) DO NOTHING`,
		event.EventID, event.ProviderEmailID, event.EventType, event.EventCreatedAt)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows == 0 {
		return false, tx.Commit()
	}
	if event.ProviderEmailID == "" {
		if err = tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO email_deliveries
			(provider, provider_email_id, status, recipient_domain, last_event_at)
		VALUES ('resend', $1, $2, NULLIF($3, ''), $4)
		ON CONFLICT (provider, provider_email_id) DO UPDATE SET
			status = EXCLUDED.status,
			recipient_domain = COALESCE(EXCLUDED.recipient_domain, email_deliveries.recipient_domain),
			last_event_at = EXCLUDED.last_event_at,
			updated_at = NOW()
		WHERE email_deliveries.last_event_at IS NULL
		   OR EXCLUDED.last_event_at >= email_deliveries.last_event_at`,
		event.ProviderEmailID, event.EventType, event.RecipientDomain, event.EventCreatedAt)
	if err != nil {
		return false, fmt.Errorf("upsert email delivery: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *emailDeliveryRepository) ListRecent(ctx context.Context, limit int) ([]service.EmailDelivery, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.client.QueryContext(ctx, `
		SELECT provider_email_id, status, COALESCE(recipient_domain, ''), last_event_at, updated_at
		FROM email_deliveries
		ORDER BY updated_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.EmailDelivery, 0, limit)
	for rows.Next() {
		var item service.EmailDelivery
		if err := rows.Scan(&item.ProviderEmailID, &item.Status, &item.RecipientDomain, &item.LastEventAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
