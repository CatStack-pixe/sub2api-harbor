package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTokenRhythmBalanceProbeAllowsSchedulingOnlyWithFreshPositiveBalance(t *testing.T) {
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(10 * time.Minute).Format(time.RFC3339)
	account := &Account{
		Platform: PlatformTokenRhythm,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			UpstreamBillingProbeEnabledExtraKey: true,
			UpstreamBillingProbeExtraKey: map[string]any{
				"status":      UpstreamBillingProbeStatusOK,
				"fresh_until": fresh,
				"data": map[string]any{
					"available_balance_cny": 1.25,
				},
			},
		},
	}
	require.True(t, tokenRhythmBalanceProbeAllowsScheduling(account, now))

	account.Extra[UpstreamBillingProbeExtraKey].(map[string]any)["data"].(map[string]any)["available_balance_cny"] = 0.0
	require.False(t, tokenRhythmBalanceProbeAllowsScheduling(account, now))

	account.Extra[UpstreamBillingProbeExtraKey].(map[string]any)["data"].(map[string]any)["available_balance_cny"] = 1.25
	account.Extra[UpstreamBillingProbeExtraKey].(map[string]any)["fresh_until"] = now.Add(-time.Second).Format(time.RFC3339)
	require.False(t, tokenRhythmBalanceProbeAllowsScheduling(account, now))
}

func TestTokenRhythmBalanceProbeDisabledPreservesScheduling(t *testing.T) {
	account := &Account{Platform: PlatformTokenRhythm, Type: AccountTypeAPIKey}
	require.True(t, tokenRhythmBalanceProbeAllowsScheduling(account, time.Now()))
}
