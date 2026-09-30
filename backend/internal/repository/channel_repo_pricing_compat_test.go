//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const compatibilityTimePricingJSON = `{"timezone":"Asia/Shanghai","weekdays_only":true,"periods":[{"start_time":"09:00:00","end_time":"12:00:00","multiplier":2}]}`

var compatibilityTimeWindowColumns = []string{
	"id", "pricing_id", "start_minute", "end_minute", "input_price", "output_price",
	"cache_write_price", "cache_read_price", "sort_order", "created_at", "updated_at",
}

func compatibilityPricing() *service.ChannelModelPricing {
	cacheWrite, cacheWrite1h, windowInput := 1e-6, 2e-6, 3e-6
	legacyMax := 99.0
	return &service.ChannelModelPricing{
		ID: 11, ChannelID: 7, Platform: "sensenova", Models: []string{"custom-model"},
		CacheWritePrice: &cacheWrite, CacheWrite1hPrice: &cacheWrite1h,
		ReasoningEffortMultipliers:   map[string]float64{"high": 1.5, "max": 4},
		MaxReasoningEffortMultiplier: &legacyMax,
		TimePricing: &service.ChannelTimePricing{
			Timezone: "Asia/Shanghai", WeekdaysOnly: true,
			Periods: []service.ChannelTimePricingPeriod{{
				StartTime: "09:00:00", EndTime: "12:00:00", Multiplier: 2,
			}},
		},
		TimeWindows: []service.PricingTimeWindow{{
			StartMinute: 540, EndMinute: 720, InputPrice: &windowInput, SortOrder: 1,
		}},
	}
}

func expectCompatibilityWindowInsert(mock sqlmock.Sqlmock, window *service.PricingTimeWindow) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery(`INSERT INTO channel_pricing_time_windows`).
		WithArgs(int64(11), window.StartMinute, window.EndMinute, window.InputPrice, window.OutputPrice,
			window.CacheWritePrice, window.CacheReadPrice, window.SortOrder)
}

func expectCompatibilityPricingRead(mock sqlmock.Sqlmock, storedJSON string, batch bool) {
	query := `(?s)SELECT .*cache_write_1h_price.*reasoning_effort_multipliers.*FROM channel_model_pricing.*channel_id = \$1`
	var arg any = int64(7)
	if batch {
		query = `(?s)SELECT .*cache_write_1h_price.*reasoning_effort_multipliers.*FROM channel_model_pricing.*channel_id = ANY`
		arg = pq.Array([]int64{7})
	}
	mock.ExpectQuery(query).WithArgs(arg).
		WillReturnRows(sqlmock.NewRows(channelModelPricingTimePricingColumns).AddRow(
			int64(11), int64(7), "sensenova", `["custom-model"]`, service.BillingModeToken,
			nil, nil, 1e-6, 2e-6, nil, nil, nil, storedJSON, nil, nil, nil, compatibilityTimePricingJSON,
			time.Time{}, time.Time{},
		))
	expectEmptyModelPricingIntervals(mock)
	mock.ExpectQuery(`SELECT id, pricing_id, start_minute, end_minute`).
		WithArgs(pq.Array([]int64{11})).
		WillReturnRows(sqlmock.NewRows(compatibilityTimeWindowColumns).AddRow(
			int64(31), int64(11), 540, 720, 3e-6, nil, nil, nil, 1, time.Time{}, time.Time{},
		))
}

func TestChannelModelPricingCompatibilityRoundTrip(t *testing.T) {
	for _, operation := range []string{"create", "update", "replace"} {
		t.Run(operation, func(t *testing.T) {
			repo, mock := newChannelModelPricingTimePricingRepo(t)
			ctx := context.Background()
			pricing := compatibilityPricing()
			stored := &capturedReasoningMultipliersJSON{}
			if operation == "update" {
				mock.ExpectBegin()
				mock.ExpectExec(`(?s)UPDATE channel_model_pricing.*cache_write_1h_price = \$6.*reasoning_effort_multipliers = \$10.*WHERE id = \$16`).
					WithArgs([]byte(`["custom-model"]`), service.BillingModeToken,
						nil, nil, 1e-6, 2e-6, nil, nil, nil, stored, nil, nil, nil,
						compatibilityTimePricingJSON, "sensenova", int64(11)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`DELETE FROM channel_pricing_time_windows WHERE pricing_id = \$1`).
					WithArgs(int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
			} else {
				if operation == "replace" {
					mock.ExpectBegin()
					mock.ExpectExec(`DELETE FROM channel_model_pricing WHERE channel_id = \$1`).
						WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
				}
				mock.ExpectQuery(`INSERT INTO channel_model_pricing .*cache_write_1h_price.*reasoning_effort_multipliers`).
					WithArgs(int64(7), "sensenova", []byte(`["custom-model"]`), service.BillingModeToken,
						nil, nil, 1e-6, 2e-6, nil, nil, nil, stored, nil, nil, nil, compatibilityTimePricingJSON).
					WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
						AddRow(int64(11), time.Time{}, time.Time{}))
			}
			expectCompatibilityWindowInsert(mock, &pricing.TimeWindows[0]).
				WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
					AddRow(int64(31), time.Time{}, time.Time{}))
			if operation != "create" {
				mock.ExpectCommit()
			}
			switch operation {
			case "create":
				require.NoError(t, repo.CreateModelPricing(ctx, pricing))
			case "update":
				require.NoError(t, repo.UpdateModelPricing(ctx, pricing))
			case "replace":
				require.NoError(t, repo.ReplaceModelPricing(ctx, 7, []service.ChannelModelPricing{*pricing}))
			}
			require.JSONEq(t, `{"high":1.5,"max":4}`, stored.value)

			// Both direct and batch reads retain the upstream fields and fork windows.
			for _, batch := range []bool{false, true} {
				expectCompatibilityPricingRead(mock, stored.value, batch)
				var loaded []service.ChannelModelPricing
				if batch {
					values, err := repo.batchLoadModelPricing(ctx, []int64{7})
					require.NoError(t, err)
					loaded = values[7]
				} else {
					var err error
					loaded, err = repo.ListModelPricing(ctx, 7)
					require.NoError(t, err)
				}
				require.Len(t, loaded, 1)
				require.Equal(t, pricing.ReasoningEffortMultipliers, loaded[0].ReasoningEffortMultipliers)
				require.Equal(t, pricing.CacheWritePrice, loaded[0].CacheWritePrice)
				require.Equal(t, pricing.CacheWrite1hPrice, loaded[0].CacheWrite1hPrice)
				require.Equal(t, pricing.TimePricing, loaded[0].TimePricing)
				require.Len(t, loaded[0].TimeWindows, 1)
				require.Equal(t, 540, loaded[0].TimeWindows[0].StartMinute)
				require.Equal(t, 720, loaded[0].TimeWindows[0].EndMinute)
				require.Equal(t, pricing.TimeWindows[0].InputPrice, loaded[0].TimeWindows[0].InputPrice)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestChannelModelPricingCompatibilityUpdateRollback(t *testing.T) {
	for _, stage := range []string{"update", "not found", "delete windows", "insert window"} {
		t.Run(stage, func(t *testing.T) {
			repo, mock := newChannelModelPricingTimePricingRepo(t)
			pricing := compatibilityPricing()
			failure := errors.New("fixture write failure")
			mock.ExpectBegin()
			update := mock.ExpectExec(`(?s)UPDATE channel_model_pricing.*reasoning_effort_multipliers = \$10.*WHERE id = \$16`).
				WithArgs([]byte(`["custom-model"]`), service.BillingModeToken,
					nil, nil, 1e-6, 2e-6, nil, nil, nil, `{"high":1.5,"max":4}`, nil, nil, nil,
					compatibilityTimePricingJSON, "sensenova", int64(11))
			switch stage {
			case "update":
				update.WillReturnError(failure)
			case "not found":
				update.WillReturnResult(sqlmock.NewResult(0, 0))
			default:
				update.WillReturnResult(sqlmock.NewResult(0, 1))
				deleteWindows := mock.ExpectExec(`DELETE FROM channel_pricing_time_windows WHERE pricing_id = \$1`).
					WithArgs(int64(11))
				if stage == "delete windows" {
					deleteWindows.WillReturnError(failure)
				} else {
					deleteWindows.WillReturnResult(sqlmock.NewResult(0, 1))
					// A later insert fails after the first succeeds: all writes must roll back.
					firstWindow := pricing.TimeWindows[0]
					firstWindow.StartMinute, firstWindow.EndMinute = 0, 60
					pricing.TimeWindows = append([]service.PricingTimeWindow{firstWindow}, pricing.TimeWindows...)
					expectCompatibilityWindowInsert(mock, &pricing.TimeWindows[0]).
						WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
							AddRow(int64(30), time.Time{}, time.Time{}))
					expectCompatibilityWindowInsert(mock, &pricing.TimeWindows[1]).WillReturnError(failure)
				}
			}
			mock.ExpectRollback()
			err := repo.UpdateModelPricing(context.Background(), pricing)
			if stage == "not found" {
				require.ErrorContains(t, err, "pricing entry not found")
			} else {
				require.ErrorIs(t, err, failure)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestChannelPricingIntervalCacheWrite1hRoundTrip(t *testing.T) {
	zero, value := 0.0, 6e-6
	for _, tc := range []struct {
		name  string
		price *float64
	}{
		{"unset", nil},
		{"zero", &zero},
		{"configured", &value},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cacheWrite1h := tc.price
			repo, mock := newChannelModelPricingTimePricingRepo(t)
			cacheWrite, cacheRead := 3e-6, 0.25e-6
			interval := &service.PricingInterval{
				PricingID: 11, MinTokens: 100, TierLabel: "large",
				CacheWritePrice: &cacheWrite, CacheWrite1hPrice: cacheWrite1h, CacheReadPrice: &cacheRead,
				SortOrder: 2,
			}
			mock.ExpectQuery(`(?s)INSERT INTO channel_pricing_intervals.*cache_write_price, cache_write_1h_price, cache_read_price`).
				WithArgs(int64(11), 100, nil, "large", nil, nil, cacheWrite, cacheWrite1h, cacheRead,
					nil, nil, nil, nil, nil, 2).
				WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
					AddRow(int64(21), time.Time{}, time.Time{}))
			require.NoError(t, createIntervalExec(context.Background(), repo.db, interval))
			mock.ExpectQuery(`(?s)SELECT .*cache_write_price, cache_write_1h_price, cache_read_price.*FROM channel_pricing_intervals`).
				WithArgs(pq.Array([]int64{11})).
				WillReturnRows(sqlmock.NewRows([]string{
					"id", "pricing_id", "min_tokens", "max_tokens", "tier_label",
					"input_price", "output_price", "cache_write_price", "cache_write_1h_price", "cache_read_price",
					"input_multiplier", "output_multiplier", "cache_write_multiplier", "cache_read_multiplier",
					"per_request_price", "sort_order", "created_at", "updated_at",
				}).AddRow(int64(21), int64(11), 100, nil, "large", nil, nil, cacheWrite, cacheWrite1h, cacheRead,
					nil, nil, nil, nil, nil, 2, time.Time{}, time.Time{}))
			loaded, err := repo.batchLoadIntervals(context.Background(), []int64{11})
			require.NoError(t, err)
			require.Len(t, loaded[11], 1)
			require.Equal(t, interval, &loaded[11][0])
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
