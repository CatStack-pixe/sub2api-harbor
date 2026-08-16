CREATE TABLE IF NOT EXISTS email_deliveries (
  id BIGSERIAL PRIMARY KEY,
  provider VARCHAR(32) NOT NULL,
  provider_email_id VARCHAR(255) NOT NULL,
  status VARCHAR(64) NOT NULL,
  recipient_domain VARCHAR(255),
  last_event_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (provider, provider_email_id)
);

CREATE TABLE IF NOT EXISTS email_delivery_events (
  id BIGSERIAL PRIMARY KEY,
  provider VARCHAR(32) NOT NULL,
  event_id VARCHAR(255) NOT NULL,
  provider_email_id VARCHAR(255),
  event_type VARCHAR(64) NOT NULL,
  event_created_at TIMESTAMPTZ NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (provider, event_id)
);

CREATE INDEX IF NOT EXISTS idx_email_deliveries_status ON email_deliveries (status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_email_delivery_events_message ON email_delivery_events (provider, provider_email_id, event_created_at DESC);
