-- Daily time-of-day channel pricing overrides. All windows are interpreted in Asia/Shanghai.
CREATE TABLE IF NOT EXISTS channel_pricing_time_windows (
    id                BIGSERIAL PRIMARY KEY,
    pricing_id        BIGINT NOT NULL REFERENCES channel_model_pricing(id) ON DELETE CASCADE,
    start_minute      SMALLINT NOT NULL,
    end_minute        SMALLINT NOT NULL,
    input_price       NUMERIC(20,12),
    output_price      NUMERIC(20,12),
    cache_write_price NUMERIC(20,12),
    cache_read_price  NUMERIC(20,12),
    sort_order        INT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT channel_pricing_time_windows_range CHECK (
        start_minute >= 0 AND start_minute < 1440 AND end_minute > 0 AND end_minute <= 1440 AND start_minute < end_minute
    )
);

CREATE INDEX IF NOT EXISTS idx_channel_pricing_time_windows_pricing_id
    ON channel_pricing_time_windows (pricing_id, sort_order, id);
