package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type senseNovaQuotaTestUpstream struct {
	statusCode int
	body       string
	calls      int
	lastReq    *http.Request
}

func (u *senseNovaQuotaTestUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	u.lastReq = req
	return &http.Response{
		StatusCode: u.statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(u.body)),
	}, nil
}

func (u *senseNovaQuotaTestUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

type senseNovaQuotaTestRepo struct {
	AccountRepository
	account *Account
	updates []map[string]any
}

func (r *senseNovaQuotaTestRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func (r *senseNovaQuotaTestRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updates = append(r.updates, updates)
	return nil
}

func newSenseNovaQuotaTestAccount(token string) *Account {
	return &Account{
		ID:       42,
		Platform: PlatformSenseNova,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
		Credentials: map[string]any{
			"api_key":      "gateway-key",
			"access_token": token,
		},
	}
}

func TestSenseNovaQuotaService_UsesAccessTokenAndPreservesPools(t *testing.T) {
	upstream := &senseNovaQuotaTestUpstream{
		statusCode: http.StatusOK,
		body: `{
			"plan": {"id": "free", "name": "Free Plan", "type": "TOKEN_PLAN_PLAN_TYPE_FREE"},
			"pools": [
				{
					"id": "pool_shared", "name": "通用积分池", "pool_type": "default",
					"model_ids": ["model-a"],
					"window_5h": {"limit": "60000", "used": "1200", "remaining": "58800", "reset_at": 1788613830},
					"window_7d": {"limit": 600000, "used": 108887, "remaining": "491113", "reset_at": "1788948630"}
				},
				{
					"id": "pool_flash", "name": "Flash-Lite积分池", "pool_type": "dedicated",
					"model_ids": ["flash-lite"],
					"window_5h": {"limit": 30000, "used": 3000, "remaining": 27000, "reset_at": 1788613830}
				}
			]
		}`,
	}
	repo := &senseNovaQuotaTestRepo{account: newSenseNovaQuotaTestAccount("oauth-access-token")}
	service := NewSenseNovaQuotaService(repo, nil, upstream, nil)

	result, err := service.QueryUsage(context.Background(), repo.account.ID)

	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, result.Persisted)
	require.Equal(t, PlatformSenseNova, result.Provider)
	require.Equal(t, "Bearer oauth-access-token", upstream.lastReq.Header.Get("Authorization"))
	require.NotContains(t, upstream.lastReq.Header.Get("Authorization"), "gateway-key")
	require.Equal(t, senseNovaQuotaURL, upstream.lastReq.URL.String())
	require.Len(t, result.Pools, 2)
	require.Equal(t, "pool_shared", result.Pools[0].ID)
	require.InDelta(t, 1200, result.Pools[0].Window5h.Used, 0.001)
	require.Equal(t, "2026-09-05T13:10:30Z", result.Pools[0].Window5h.ResetAt)
	require.Equal(t, "2026-09-09T10:10:30Z", result.Pools[0].Window7d.ResetAt)
	require.Equal(t, "pool_flash", result.Pools[1].ID)
	require.Len(t, repo.updates, 1)
	snapshot, ok := repo.updates[0][SenseNovaQuotaSnapshotExtraKey].(*SenseNovaQuotaSnapshot)
	require.True(t, ok)
	require.Len(t, snapshot.Pools, 2)
}

func TestSenseNovaQuotaService_RequiresSeparateAccessToken(t *testing.T) {
	upstream := &senseNovaQuotaTestUpstream{statusCode: http.StatusOK}
	repo := &senseNovaQuotaTestRepo{account: newSenseNovaQuotaTestAccount("")}
	service := NewSenseNovaQuotaService(repo, nil, upstream, nil)

	result, err := service.QueryUsage(context.Background(), repo.account.ID)

	require.Nil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "SENSENOVA_QUOTA_NO_ACCESS_TOKEN")
	require.Zero(t, upstream.calls)
}

func TestSenseNovaQuotaService_AuthFailureDoesNotOverwriteSnapshot(t *testing.T) {
	upstream := &senseNovaQuotaTestUpstream{
		statusCode: http.StatusUnauthorized,
		body:       `{"error":"expired"}`,
	}
	repo := &senseNovaQuotaTestRepo{account: newSenseNovaQuotaTestAccount("expired-token")}
	service := NewSenseNovaQuotaService(repo, nil, upstream, nil)

	result, err := service.QueryUsage(context.Background(), repo.account.ID)

	require.NoError(t, err)
	require.False(t, result.Success)
	require.False(t, result.CredentialValid)
	require.Equal(t, http.StatusUnauthorized, result.StatusCode)
	require.Empty(t, repo.updates)
}
