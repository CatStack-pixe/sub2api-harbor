package service

import (
	"encoding/json"
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

func TestChannelModelPricingCompatibilityClone(t *testing.T) {
	pricing := ChannelModelPricing{
		Models:                     []string{"custom-model"},
		CacheWrite1hPrice:          timeWindowFloat(6e-6),
		ReasoningEffortMultipliers: map[string]float64{"high": 1.5, "max": 4},
		Intervals: []PricingInterval{{
			MinTokens: 0, CacheWrite1hPrice: timeWindowFloat(12e-6),
		}},
		TimeWindows: []PricingTimeWindow{{
			StartMinute: 540, EndMinute: 720, CacheWritePrice: timeWindowFloat(3e-6),
		}},
		TimePricing: &ChannelTimePricing{
			Timezone: "Asia/Shanghai", WeekdaysOnly: true,
			Periods: []ChannelTimePricingPeriod{{
				StartTime: "09:00:00", EndTime: "12:00:00", Multiplier: 2,
			}},
		},
	}

	cloned := pricing.Clone()
	require.Equal(t, pricing, cloned)
	cloned.Models[0] = "changed"
	cloned.ReasoningEffortMultipliers["high"] = 99
	cloned.Intervals[0].MinTokens = 10
	cloned.TimeWindows[0].StartMinute = 600
	cloned.TimePricing.Periods[0].Multiplier = 99
	require.Equal(t, "custom-model", pricing.Models[0])
	require.Equal(t, 1.5, pricing.ReasoningEffortMultipliers["high"])
	require.Equal(t, 0, pricing.Intervals[0].MinTokens)
	require.Equal(t, 540, pricing.TimeWindows[0].StartMinute)
	require.Equal(t, 2.0, pricing.TimePricing.Periods[0].Multiplier)

	resolved := pricing.ApplyTimeWindow(time.Date(2026, 9, 30, 2, 0, 0, 0, time.UTC))
	require.Equal(t, pricing.CacheWrite1hPrice, resolved.CacheWrite1hPrice)
	require.Equal(t, pricing.Intervals[0].CacheWrite1hPrice, resolved.Intervals[0].CacheWrite1hPrice)
	require.Equal(t, 3e-6, *resolved.CacheWritePrice)
	require.Equal(t, 3e-6, *resolved.Intervals[0].CacheWritePrice)
	require.Nil(t, pricing.CacheWritePrice)
	require.Nil(t, pricing.Intervals[0].CacheWritePrice)
	require.Equal(t, pricing.ReasoningEffortMultipliers, resolved.ReasoningEffortMultipliers)
	require.Equal(t, pricing.TimePricing, resolved.TimePricing)
	resolved.ReasoningEffortMultipliers["max"] = 99
	require.Equal(t, 4.0, pricing.ReasoningEffortMultipliers["max"])

	encoded, err := json.Marshal(pricing)
	require.NoError(t, err)
	var decoded ChannelModelPricing
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, pricing, decoded)
	require.NotContains(t, string(encoded), "max_reasoning_effort_multiplier")
}
