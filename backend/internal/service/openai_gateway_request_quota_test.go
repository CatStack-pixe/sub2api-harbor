//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReserveAccountRequestQuota_AllowsAccountsWithoutLimit(t *testing.T) {
	svc := &OpenAIGatewayService{}

	allowed, err := svc.ReserveAccountRequestQuota(context.Background(), &Account{ID: 1})

	require.NoError(t, err)
	require.True(t, allowed)
}

func TestReserveAccountRequestQuota_FailsClosedWithoutRepository(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{
		ID:    1,
		Extra: map[string]any{"request_quota_limit": 100},
	}

	allowed, err := svc.ReserveAccountRequestQuota(context.Background(), account)

	require.ErrorContains(t, err, "request quota storage is unavailable")
	require.False(t, allowed)
}
