//go:build integration

package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Exercise the actual Lua in the shared real-Redis integration harness, including six-argument callers.
func TestInflightReservation_StrictLua_RealRedis(t *testing.T) {
	tests := []struct {
		name    string
		strict  int // -1 omits the optional argument.
		balance float64
		pending float64
		expired bool
		allowed bool
	}{
		{name: "omitted flag preserves first estimate allowance", strict: -1, balance: 10, allowed: true},
		{name: "zero flag preserves first estimate allowance", strict: 0, balance: 10, allowed: true},
		{name: "strict rejects insufficient empty reservation", strict: 1, balance: 10},
		{name: "strict admits exact empty coverage", strict: 1, balance: 90, allowed: true},
		{name: "strict includes pending charges", strict: 1, balance: 100, pending: 20},
		{name: "strict admits exact coverage including pending", strict: 1, balance: 110, pending: 20, allowed: true},
		{name: "strict rejects insufficient balance after pruning", strict: 1, balance: 10, pending: 90, expired: true},
		{name: "legacy preserves allowance after pruning", strict: -1, balance: 10, pending: 90, expired: true, allowed: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cache := &billingCache{rdb: testRedis(t)}
			zkey, hkey := billingInflightKeys(81)
			now := time.Now().UnixMilli()
			ttlMs := time.Minute.Milliseconds()
			expectedAmounts := map[string]string{}
			expectedPending := tc.pending
			if tc.pending > 0 {
				expiry := now + ttlMs
				if tc.expired {
					expiry = now - 1000
					expectedPending = 0
				} else {
					expectedAmounts["held"] = strconv.FormatFloat(tc.pending, 'f', -1, 64)
				}
				require.NoError(t, cache.rdb.ZAdd(ctx, zkey, redis.Z{Score: float64(expiry), Member: "held"}).Err())
				require.NoError(t, cache.rdb.HSet(ctx, hkey, "held", strconv.FormatFloat(tc.pending, 'f', -1, 64)).Err())
			}
			args := []any{now, now + ttlMs, "requested", "90", strconv.FormatFloat(tc.balance, 'f', -1, 64), ttlMs}
			if tc.strict >= 0 {
				args = append(args, tc.strict)
			}
			reply, err := reserveInflightBalanceScript.Run(ctx, cache.rdb, []string{zkey, hkey}, args...).Slice()
			require.NoError(t, err)
			require.Len(t, reply, 3)
			expectedAllowed := int64(0)
			if tc.allowed {
				expectedAllowed = 1
				expectedAmounts["requested"] = "90"
			}
			require.Equal(t, expectedAllowed, reply[0])
			pending, err := strconv.ParseFloat(reply[1].(string), 64)
			require.NoError(t, err)
			require.Equal(t, expectedPending, pending)
			expectedPreviousCount := int64(0)
			if tc.pending > 0 && !tc.expired {
				expectedPreviousCount = 1
			}
			require.Equal(t, expectedPreviousCount, reply[2])
			amounts, err := cache.rdb.HGetAll(ctx, hkey).Result()
			require.NoError(t, err)
			require.Equal(t, expectedAmounts, amounts, "rejection must not register a charge and pruning must remove expired amounts")
			require.Equal(t, int64(len(expectedAmounts)), inflightCount(t, cache, 81))
		})
	}
}
