//go:build unit

package repository

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

var channelModelPricingTimePricingColumns = []string{
	"id", "channel_id", "platform", "models", "billing_mode", "input_price", "output_price",
	"cache_write_price", "cache_write_1h_price", "cache_write_12h_price", "cache_read_price", "fast_multiplier", "flex_multiplier", "ultrafast_multiplier", "reasoning_effort_multipliers", "image_input_price", "image_output_price",
	"per_request_price", "time_pricing", "created_at", "updated_at",
}

const channelModelPricingTimePricingJSON = `{"timezone":"Asia/Shanghai","periods":[{"start_time":"09:00","end_time":"12:00","multiplier":2}]}`

func newChannelModelPricingTimePricingRepo(t *testing.T) (*channelRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return &channelRepository{db: db}, mock
}

func modelPricingTimePricingRow(timePricing any) *sqlmock.Rows {
	return sqlmock.NewRows(channelModelPricingTimePricingColumns).AddRow(
		int64(11), int64(7), "openai", `["gpt-5"]`, service.BillingModeToken,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, "{}", nil, nil, nil, timePricing,
		time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC),
	)
}

func expectEmptyModelPricingIntervals(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT id, pricing_id, min_tokens, max_tokens, tier_label`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
}

func expectEmptyModelPricingTimeWindows(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT id, pricing_id, start_minute, end_minute`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
}

func TestChannelModelPricingTimePricingListRoundTrip(t *testing.T) {
	repo, mock := newChannelModelPricingTimePricingRepo(t)
	mock.ExpectQuery(`(?s)SELECT .*per_request_price, time_pricing, created_at, updated_at.*FROM channel_model_pricing.*channel_id = \$1`).
		WithArgs(int64(7)).
		WillReturnRows(modelPricingTimePricingRow(channelModelPricingTimePricingJSON))
	expectEmptyModelPricingIntervals(mock)
	expectEmptyModelPricingTimeWindows(mock)

	pricing, err := repo.ListModelPricing(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, pricing, 1)
	require.NotNil(t, pricing[0].TimePricing)
	require.Equal(t, "Asia/Shanghai", pricing[0].TimePricing.Timezone)
	require.Len(t, pricing[0].TimePricing.Periods, 1)
	require.Equal(t, 2.0, pricing[0].TimePricing.Periods[0].Multiplier)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelModelPricingTimePricingListNullAndMalformed(t *testing.T) {
	t.Run("SQL NULL maps to nil", func(t *testing.T) {
		repo, mock := newChannelModelPricingTimePricingRepo(t)
		mock.ExpectQuery(`(?s)SELECT .*per_request_price, time_pricing, created_at, updated_at.*FROM channel_model_pricing.*channel_id = \$1`).
			WithArgs(int64(7)).
			WillReturnRows(modelPricingTimePricingRow(nil))
		expectEmptyModelPricingIntervals(mock)
		expectEmptyModelPricingTimeWindows(mock)

		pricing, err := repo.ListModelPricing(context.Background(), 7)
		require.NoError(t, err)
		require.Len(t, pricing, 1)
		require.Nil(t, pricing[0].TimePricing)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("malformed JSON returns repository error", func(t *testing.T) {
		repo, mock := newChannelModelPricingTimePricingRepo(t)
		mock.ExpectQuery(`(?s)SELECT .*per_request_price, time_pricing, created_at, updated_at.*FROM channel_model_pricing.*channel_id = \$1`).
			WithArgs(int64(7)).
			WillReturnRows(modelPricingTimePricingRow(`{"timezone":`))

		_, err := repo.ListModelPricing(context.Background(), 7)
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "unmarshal time pricing"), "unexpected error: %v", err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestChannelModelPricingTimePricingCreateAndUpdateRoundTrip(t *testing.T) {
	cacheWrite12hPrice := 12.5
	ultrafastMultiplier := 1.75
	windowInputPrice := 0.25
	pricing := &service.ChannelModelPricing{
		ID:                  11,
		ChannelID:           7,
		Platform:            "openai",
		Models:              []string{"gpt-5"},
		CacheWrite12hPrice:  &cacheWrite12hPrice,
		UltrafastMultiplier: &ultrafastMultiplier,
		TimePricing: &service.ChannelTimePricing{
			Timezone: "Asia/Shanghai",
			Periods: []service.ChannelTimePricingPeriod{{
				StartTime: "09:00", EndTime: "12:00", Multiplier: 2,
			}},
		},
		TimeWindows: []service.PricingTimeWindow{{
			StartMinute: 540, EndMinute: 720, InputPrice: &windowInputPrice, SortOrder: 1,
		}},
	}

	t.Run("create writes JSON", func(t *testing.T) {
		repo, mock := newChannelModelPricingTimePricingRepo(t)
		mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO channel_model_pricing (channel_id, platform, models, billing_mode, input_price, output_price, cache_write_price, cache_write_1h_price, cache_write_12h_price, cache_read_price, fast_multiplier, flex_multiplier, ultrafast_multiplier, reasoning_effort_multipliers, image_input_price, image_output_price, per_request_price, time_pricing)")).
			WithArgs(
				int64(7), "openai", []byte(`["gpt-5"]`), service.BillingModeToken,
				nil, nil, nil, nil, cacheWrite12hPrice, nil, nil, nil, ultrafastMultiplier, "{}", nil, nil, nil, channelModelPricingTimePricingJSON,
			).
			WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(11), time.Time{}, time.Time{}))
		mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO channel_pricing_time_windows")).
			WithArgs(int64(11), 540, 720, windowInputPrice, nil, nil, nil, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(22), time.Time{}, time.Time{}))

		require.NoError(t, repo.CreateModelPricing(context.Background(), pricing))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("update writes JSON and entry ID", func(t *testing.T) {
		repo, mock := newChannelModelPricingTimePricingRepo(t)
		mock.ExpectBegin()
		mock.ExpectExec(`(?s)UPDATE channel_model_pricing.*cache_write_12h_price = \$7.*ultrafast_multiplier = \$11.*per_request_price = \$15, time_pricing = \$16, platform = \$17.*WHERE id = \$18`).
			WithArgs(
				[]byte(`["gpt-5"]`), service.BillingModeToken,
				nil, nil, nil, nil, cacheWrite12hPrice, nil, nil, nil, ultrafastMultiplier, "{}", nil, nil, nil, channelModelPricingTimePricingJSON, "openai", int64(11),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`DELETE FROM channel_pricing_time_windows WHERE pricing_id = \$1`).
			WithArgs(int64(11)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO channel_pricing_time_windows")).
			WithArgs(int64(11), 540, 720, windowInputPrice, nil, nil, nil, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(22), time.Time{}, time.Time{}))
		mock.ExpectCommit()

		require.NoError(t, repo.UpdateModelPricing(context.Background(), pricing))
		require.Equal(t, int64(11), pricing.TimeWindows[0].PricingID)
		require.Equal(t, int64(22), pricing.TimeWindows[0].ID)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestChannelModelPricingUpdateRollsBackOnMissingRowOrWindowFailure(t *testing.T) {
	newPricing := func() *service.ChannelModelPricing {
		return &service.ChannelModelPricing{
			ID: 11, Models: []string{"gpt-5"},
			TimeWindows: []service.PricingTimeWindow{{StartMinute: 540, EndMinute: 720}},
		}
	}

	t.Run("zero updated rows rolls back before replacing windows", func(t *testing.T) {
		repo, mock := newChannelModelPricingTimePricingRepo(t)
		mock.ExpectBegin()
		mock.ExpectExec(`(?s)UPDATE channel_model_pricing.*cache_write_12h_price = \$7.*ultrafast_multiplier = \$11.*WHERE id = \$18`).
			WithArgs([]byte(`["gpt-5"]`), service.BillingModeToken,
				nil, nil, nil, nil, nil, nil, nil, nil, nil, "{}", nil, nil, nil, nil, "", int64(11)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectRollback()

		err := repo.UpdateModelPricing(context.Background(), newPricing())
		require.ErrorContains(t, err, "pricing entry not found: 11")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("window insert failure rolls back main update and deletion", func(t *testing.T) {
		repo, mock := newChannelModelPricingTimePricingRepo(t)
		mock.ExpectBegin()
		mock.ExpectExec(`(?s)UPDATE channel_model_pricing.*cache_write_12h_price = \$7.*ultrafast_multiplier = \$11.*WHERE id = \$18`).
			WithArgs([]byte(`["gpt-5"]`), service.BillingModeToken,
				nil, nil, nil, nil, nil, nil, nil, nil, nil, "{}", nil, nil, nil, nil, "", int64(11)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`DELETE FROM channel_pricing_time_windows WHERE pricing_id = \$1`).
			WithArgs(int64(11)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO channel_pricing_time_windows")).
			WithArgs(int64(11), 540, 720, nil, nil, nil, nil, 0).
			WillReturnError(errors.New("insert failed"))
		mock.ExpectRollback()

		err := repo.UpdateModelPricing(context.Background(), newPricing())
		require.ErrorContains(t, err, "insert pricing time window")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("rows affected error rolls back", func(t *testing.T) {
		repo, mock := newChannelModelPricingTimePricingRepo(t)
		mock.ExpectBegin()
		mock.ExpectExec(`(?s)UPDATE channel_model_pricing.*WHERE id = \$18`).
			WithArgs([]byte(`["gpt-5"]`), service.BillingModeToken,
				nil, nil, nil, nil, nil, nil, nil, nil, nil, "{}", nil, nil, nil, nil, "", int64(11)).
			WillReturnResult(sqlmock.NewErrorResult(errors.New("rows unavailable")))
		mock.ExpectRollback()

		err := repo.UpdateModelPricing(context.Background(), newPricing())
		require.ErrorContains(t, err, "check updated model pricing rows")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestChannelModelPricingTimePricingCreateAndUpdateWriteNullWhenDisabled(t *testing.T) {
	tests := []struct {
		name        string
		timePricing *service.ChannelTimePricing
	}{
		{name: "nil", timePricing: nil},
		{name: "empty periods", timePricing: &service.ChannelTimePricing{Timezone: "Asia/Shanghai"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newPricing := func() *service.ChannelModelPricing {
				return &service.ChannelModelPricing{
					ID:          11,
					ChannelID:   7,
					Platform:    "openai",
					Models:      []string{"gpt-5"},
					TimePricing: tt.timePricing,
				}
			}

			t.Run("create writes SQL NULL", func(t *testing.T) {
				repo, mock := newChannelModelPricingTimePricingRepo(t)
				mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO channel_model_pricing (channel_id, platform, models, billing_mode, input_price, output_price, cache_write_price, cache_write_1h_price, cache_write_12h_price, cache_read_price, fast_multiplier, flex_multiplier, ultrafast_multiplier, reasoning_effort_multipliers, image_input_price, image_output_price, per_request_price, time_pricing)")).
					WithArgs(
						int64(7), "openai", []byte(`["gpt-5"]`), service.BillingModeToken,
						nil, nil, nil, nil, nil, nil, nil, nil, nil, "{}", nil, nil, nil, nil,
					).
					WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(11), time.Time{}, time.Time{}))

				require.NoError(t, repo.CreateModelPricing(context.Background(), newPricing()))
				require.NoError(t, mock.ExpectationsWereMet())
			})

			t.Run("update writes SQL NULL", func(t *testing.T) {
				repo, mock := newChannelModelPricingTimePricingRepo(t)
				mock.ExpectBegin()
				mock.ExpectExec(`(?s)UPDATE channel_model_pricing.*per_request_price = \$15, time_pricing = \$16, platform = \$17.*WHERE id = \$18`).
					WithArgs(
						[]byte(`["gpt-5"]`), service.BillingModeToken,
						nil, nil, nil, nil, nil, nil, nil, nil, nil, "{}", nil, nil, nil, nil, "openai", int64(11),
					).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`DELETE FROM channel_pricing_time_windows WHERE pricing_id = \$1`).
					WithArgs(int64(11)).
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectCommit()

				require.NoError(t, repo.UpdateModelPricing(context.Background(), newPricing()))
				require.NoError(t, mock.ExpectationsWereMet())
			})
		})
	}
}
