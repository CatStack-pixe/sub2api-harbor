package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func timeWindowFloat(v float64) *float64 { return &v }

func TestChannelModelPricingApplyTimeWindow(t *testing.T) {
	baseInput := 1.5e-6
	baseOutput := 4.5e-6
	peakInput := 3e-6
	peakOutput := 9e-6
	pricing := ChannelModelPricing{
		InputPrice:  &baseInput,
		OutputPrice: &baseOutput,
		TimeWindows: []PricingTimeWindow{{
			StartMinute: 9 * 60, EndMinute: 12 * 60,
			InputPrice: &peakInput, OutputPrice: &peakOutput,
		}},
	}
	beijing, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		at   time.Time
		want float64
	}{
		{name: "before peak", at: time.Date(2026, 8, 17, 8, 59, 0, 0, beijing), want: baseInput},
		{name: "peak start included", at: time.Date(2026, 8, 17, 9, 0, 0, 0, beijing), want: peakInput},
		{name: "peak end excluded", at: time.Date(2026, 8, 17, 12, 0, 0, 0, beijing), want: baseInput},
		{name: "utc instant uses Beijing clock", at: time.Date(2026, 8, 17, 1, 30, 0, 0, time.UTC), want: peakInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved := pricing.ApplyTimeWindow(tc.at)
			require.NotNil(t, resolved.InputPrice)
			require.InDelta(t, tc.want, *resolved.InputPrice, 1e-12)
		})
	}
	resolved := pricing.ApplyTimeWindow(time.Date(2026, 8, 17, 10, 0, 0, 0, beijing))
	require.InDelta(t, peakOutput, *resolved.OutputPrice, 1e-12)
	require.InDelta(t, baseInput, *pricing.InputPrice, 1e-12)
	require.InDelta(t, baseOutput, *pricing.OutputPrice, 1e-12)
}

func TestValidatePricingTimeWindows(t *testing.T) {
	valid := []PricingTimeWindow{
		{StartMinute: 9 * 60, EndMinute: 12 * 60, InputPrice: timeWindowFloat(1)},
		{StartMinute: 14 * 60, EndMinute: 18 * 60, OutputPrice: timeWindowFloat(1)},
	}
	require.NoError(t, ValidatePricingTimeWindows(valid))
	require.Error(t, ValidatePricingTimeWindows([]PricingTimeWindow{{StartMinute: 0, EndMinute: 0, InputPrice: timeWindowFloat(1)}}))
	require.Error(t, ValidatePricingTimeWindows([]PricingTimeWindow{{StartMinute: 0, EndMinute: 60}}))
	require.Error(t, ValidatePricingTimeWindows([]PricingTimeWindow{
		{StartMinute: 0, EndMinute: 60, InputPrice: timeWindowFloat(1)},
		{StartMinute: 30, EndMinute: 90, InputPrice: timeWindowFloat(1)},
	}))
}
