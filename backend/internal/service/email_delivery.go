package service

import (
	"context"
	"time"
)

type ResendDeliveryEvent struct {
	EventID         string
	EventType       string
	ProviderEmailID string
	RecipientDomain string
	EventCreatedAt  time.Time
}

type EmailDelivery struct {
	ProviderEmailID string    `json:"provider_email_id"`
	Status          string    `json:"status"`
	RecipientDomain string    `json:"recipient_domain"`
	LastEventAt     time.Time `json:"last_event_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type EmailDeliveryRepository interface {
	RecordResendEvent(ctx context.Context, event ResendDeliveryEvent) (bool, error)
	ListRecent(ctx context.Context, limit int) ([]EmailDelivery, error)
}
