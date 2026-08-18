package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTokenRhythmBalanceProbeAllowsSchedulingOnlyWithFreshPositiveBalance(t *testing.T) {
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	newAccount := func(balance float64, freshUntil time.Time) *Account {
		return &Account{
			Platform: PlatformTokenRhythm,
			Type:     AccountTypeAPIKey,
			Extra: map[string]any{
				UpstreamBillingProbeEnabledExtraKey: true,
				UpstreamBillingProbeExtraKey: map[string]any{
					"status":      UpstreamBillingProbeStatusOK,
					"fresh_until": freshUntil.Format(time.RFC3339),
					"data": map[string]any{
						"available_balance_cny": balance,
					},
				},
			},
		}
	}
	account := newAccount(1.25, now.Add(10*time.Minute))
	require.True(t, tokenRhythmBalanceProbeAllowsScheduling(account, now))

	require.False(t, tokenRhythmBalanceProbeAllowsScheduling(newAccount(0, now.Add(10*time.Minute)), now))

	require.False(t, tokenRhythmBalanceProbeAllowsScheduling(newAccount(1.25, now.Add(-time.Second)), now))
}

func TestTokenRhythmBalanceProbeDisabledPreservesScheduling(t *testing.T) {
	account := &Account{Platform: PlatformTokenRhythm, Type: AccountTypeAPIKey}
	require.True(t, tokenRhythmBalanceProbeAllowsScheduling(account, time.Now()))
}
