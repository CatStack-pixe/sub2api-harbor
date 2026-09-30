package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

const tierflowTestStatus = `{"success":true,"data":{"quota_per_unit":500000}}`
const tierflowTestBalance = `{"success":true,"data":{"quota":15000000,"used_quota":250000,"request_count":7,"uid":"123","token":"never-persist"}}`

func newTierflowBalanceTestAccount() *Account {
	return &Account{
		ID:          941,
		Platform:    PlatformTierflow,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 3,
		Credentials: map[string]any{
			"api_key":          "test-api-key",
			"tierflow_cookie":  "test-session",
			"tierflow_user_id": "123",
		},
	}
}

func tierflowTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestTierflowCookieCredentialsNormalizeAndRejectHeaderInjection(t *testing.T) {
	for _, cookie := range []string{"test-session", "session=test-session", "other=ignored; session=test-session; theme=dark"} {
		session, userID, err := TierflowCookieCredentials(map[string]any{"tierflow_cookie": cookie, "tierflow_user_id": "00123"})
		require.NoError(t, err)
		require.Equal(t, "test-session", session)
		require.Equal(t, "123", userID)
	}
	for _, cookie := range []string{"", "theme=dark; other=cookie", "session=; session=secret-value", "session=a; session=secret-value", "secret-value\r\nX: injected", "secret-value other", "secret-value\x00", "secret-value,other"} {
		_, _, err := TierflowCookieCredentials(map[string]any{"tierflow_cookie": cookie, "tierflow_user_id": "123"})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret-value")
	}
	for _, userID := range []any{"", "0", "-1", "+1", "12 3", "123\r\nCookie: secret-value", "18446744073709551616", 123} {
		_, _, err := TierflowCookieCredentials(map[string]any{"tierflow_cookie": "test-session", "tierflow_user_id": userID})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret-value")
	}
}

func TestSanitizeTierflowCredentialsAndKeepSessionOnPartialEdit(t *testing.T) {
	credentials := SanitizeStoredCredentials(PlatformTierflow, map[string]any{
		"api_key":          "test-api-key",
		"tierflow_cookie":  "other=ignored; session=test-session",
		"tierflow_user_id": "00123",
		"password":         "must-not-persist",
	})
	require.Equal(t, "test-session", credentials["tierflow_cookie"])
	require.Equal(t, "123", credentials["tierflow_user_id"])
	require.NotContains(t, credentials, "password")
	require.True(t, IsSensitiveCredentialKey("tierflow_cookie"))
	merged := MergePreservingSensitiveCreds(credentials, map[string]any{"tierflow_user_id": "123"})
	require.Equal(t, "test-session", merged["tierflow_cookie"])
	stripped := SanitizeStoredCredentials(PlatformOpenAI, credentials)
	require.NotContains(t, stripped, "tierflow_cookie")
	require.NotContains(t, stripped, "tierflow_user_id")
}

func TestParseTierflowBalanceUsesObservedQuotaAndProviderUnit(t *testing.T) {
	result, err := ParseTierflowBalanceResponse([]byte(tierflowTestBalance), []byte(tierflowTestStatus))
	require.NoError(t, err)
	require.True(t, result.IsAvailable)
	require.Equal(t, 30.0, result.RemainingBalance)
	require.Equal(t, 0.5, result.TotalUsage)
	require.Equal(t, "CNY", result.Currency)
	require.Equal(t, int64(7), result.RequestCount)
	require.Equal(t, 500000.0, result.QuotaPerUnit)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "never-persist")
	require.NotContains(t, string(encoded), "uid")
	result, err = ParseTierflowBalanceResponse([]byte(`{"success":true,"data":{"quota":"0","used_quota":"10","request_count":0}}`), []byte(`{"success":true,"data":{"quota_per_unit":"10"}}`))
	require.NoError(t, err)
	require.Zero(t, result.RemainingBalance)
	require.Equal(t, 1.0, result.TotalUsage)
}

func TestParseTierflowBalanceRejectsMissingInvalidAndSecretErrorData(t *testing.T) {
	for _, body := range []string{
		`not json`,
		`{"success":false,"message":"secret-value"}`,
		`{"success":true,"data":{}}`,
		`{"success":true,"data":{"quota":null,"used_quota":0,"request_count":0}}`,
		`{"success":true,"data":{"quota":0,"request_count":0}}`,
		`{"success":true,"data":{"quota":"NaN","used_quota":0,"request_count":0}}`,
		`{"success":true,"data":{"quota":"Inf","used_quota":0,"request_count":0}}`,
		`{"success":true,"data":{"quota":1,"used_quota":-1,"request_count":0}}`,
		`{"success":true,"data":{"quota":1,"used_quota":0}}`,
	} {
		_, err := ParseTierflowBalanceResponse([]byte(body), []byte(tierflowTestStatus))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret-value")
	}
	for _, body := range []string{`{}`, `{"success":true,"data":{}}`, `{"success":true,"data":{"quota_per_unit":0}}`, `{"success":true,"data":{"quota_per_unit":"NaN"}}`} {
		_, err := ParseTierflowBalanceResponse([]byte(tierflowTestBalance), []byte(body))
		require.Error(t, err)
	}
}

func TestFetchTierflowBalanceHonorsProxyAndProtectsConsoleCredentials(t *testing.T) {
	account := newTierflowBalanceTestAccount()
	proxyID := int64(7)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "127.0.0.1", Port: 8081}
	// Admin header overrides and custom API roots never redirect console secrets.
	account.Credentials["base_url"] = "https://untrusted.example/v1"
	account.Credentials["header_overrides"] = map[string]any{"Cookie": "untrusted"}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		tierflowTestResponse(http.StatusOK, tierflowTestBalance),
		tierflowTestResponse(http.StatusOK, tierflowTestStatus),
	}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	result, err := svc.FetchTierflowBalance(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, 30.0, result.RemainingBalance)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, tierflowBalanceURL, upstream.requests[0].URL.String())
	require.Equal(t, "session=test-session", upstream.requests[0].Header.Get("Cookie"))
	require.Equal(t, "123", upstream.requests[0].Header.Get("TF-User"))
	require.Equal(t, tierflowStatusURL, upstream.requests[1].URL.String())
	require.Empty(t, upstream.requests[1].Header.Get("Cookie"))
	require.Empty(t, upstream.requests[1].Header.Get("TF-User"))
	for _, req := range upstream.requests {
		require.Empty(t, req.Header.Get("Authorization"))
		require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
		_, hasDeadline := req.Context().Deadline()
		require.True(t, hasDeadline)
	}
	require.Equal(t, account.Proxy.URL(), upstream.lastProxyURL)
	require.Equal(t, account.ID, upstream.lastAccountID)
	require.Equal(t, account.Concurrency, upstream.lastConcurrency)
	require.Equal(t, StatusActive, account.Status)
	require.True(t, account.Schedulable)

	account.Proxy = nil
	_, err = svc.FetchTierflowBalance(context.Background(), account.ID)
	require.ErrorContains(t, err, "proxy is unavailable")
	require.Len(t, upstream.requests, 2)
}

func TestFetchTierflowBalanceErrorsAreBoundedAndDoNotEchoSecrets(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests} {
		account := newTierflowBalanceTestAccount()
		repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
		upstream := &httpUpstreamRecorder{resp: tierflowTestResponse(status, "secret-value")}
		svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
		_, err := svc.FetchTierflowBalance(context.Background(), account.ID)
		require.Error(t, err)
		require.Equal(t, status, infraerrors.Code(err))
		require.NotContains(t, err.Error(), "secret-value")
	}
	account := newTierflowBalanceTestAccount()
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	upstream := &httpUpstreamRecorder{resp: tierflowTestResponse(http.StatusOK, strings.Repeat("x", tierflowBalanceBodyLimit+1))}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	_, err := svc.FetchTierflowBalance(context.Background(), account.ID)
	require.ErrorContains(t, err, "exceeds")
	upstream.err = errors.New("proxy password secret-value; Cookie: test-session")
	_, err = svc.FetchTierflowBalance(context.Background(), account.ID)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-value")
	require.NotContains(t, err.Error(), "test-session")
}

func TestTierflowWalletProbeAndUsageShareSnapshotWithoutSyncingRate(t *testing.T) {
	account := newTierflowBalanceTestAccount()
	rate := 1.25
	account.RateMultiplier = &rate
	account.Extra = map[string]any{UpstreamBillingProbeEnabledExtraKey: true, UpstreamBillingRateSyncEnabledExtraKey: true}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		tierflowTestResponse(http.StatusOK, tierflowTestBalance),
		tierflowTestResponse(http.StatusOK, tierflowTestStatus),
		tierflowTestResponse(http.StatusOK, `{"success":true,"data":{"quota":0,"used_quota":15250000,"request_count":8}}`),
		tierflowTestResponse(http.StatusOK, tierflowTestStatus),
	}}
	probe := newUpstreamBillingProbeTestService(repo, upstream, &upstreamBillingProbeSettingRepo{})
	usageSvc := &AccountUsageService{accountRepo: repo, upstreamBillingProbeService: probe}
	usage, err := usageSvc.GetUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, usage.TierflowBalance)
	require.Equal(t, 30.0, usage.TierflowBalance.RemainingBalance)
	require.True(t, tierflowBalanceProbeAllowsScheduling(account, time.Now()))
	snapshot := decodeUpstreamBillingProbeSnapshot(account.Extra)
	require.NotNil(t, snapshot)
	require.NotContains(t, snapshot.Data, "resolved_rate_multiplier")
	require.NotContains(t, snapshot.Data, "token")
	require.Nil(t, snapshot.SyncedRateMultiplier)
	require.Equal(t, 1.25, *account.RateMultiplier)
	_, err = usageSvc.GetUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.Len(t, upstream.requests, 2, "fresh usage snapshot should not query the provider again")
	usage, err = usageSvc.GetUsage(context.Background(), account.ID, true)
	require.NoError(t, err)
	require.Zero(t, usage.TierflowBalance.RemainingBalance)
	require.Len(t, upstream.requests, 4)
	require.False(t, tierflowBalanceProbeAllowsScheduling(account, time.Now()))
}

func TestTierflowWalletGateRejectsStaleFailedAndInvalidBalances(t *testing.T) {
	now := time.Now()
	account := newTierflowBalanceTestAccount()
	require.True(t, tierflowBalanceProbeAllowsScheduling(account, now), "key-only accounts preserve scheduling")
	account.Extra = map[string]any{UpstreamBillingProbeEnabledExtraKey: true}
	require.False(t, tierflowBalanceProbeAllowsScheduling(account, now))
	for _, available := range []float64{0, -1, math.Inf(1), math.NaN()} {
		account.Extra[UpstreamBillingProbeExtraKey] = &UpstreamBillingProbeSnapshot{Status: UpstreamBillingProbeStatusOK, FreshUntil: probeTimePtr(now.Add(time.Minute)), Data: map[string]any{"remaining_balance": available}}
		require.False(t, tierflowBalanceProbeAllowsScheduling(account, now))
	}
	account.Extra[UpstreamBillingProbeExtraKey] = &UpstreamBillingProbeSnapshot{Status: UpstreamBillingProbeStatusOK, FreshUntil: probeTimePtr(now.Add(-time.Second)), Data: map[string]any{"remaining_balance": 1.0}}
	require.False(t, tierflowBalanceProbeAllowsScheduling(account, now))
	account.Extra[UpstreamBillingProbeExtraKey] = &UpstreamBillingProbeSnapshot{Status: UpstreamBillingProbeStatusFailed, FreshUntil: probeTimePtr(now.Add(time.Minute)), Data: map[string]any{"remaining_balance": 1.0}}
	require.False(t, tierflowBalanceProbeAllowsScheduling(account, now))
}

func TestTierflowWalletGateAllowsFreshDecodedMetadata(t *testing.T) {
	now := time.Now()
	account := &Account{
		ID:       941,
		Platform: PlatformTierflow,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			UpstreamBillingProbeEnabledExtraKey: true,
			UpstreamBillingProbeExtraKey: map[string]any{
				"status":      UpstreamBillingProbeStatusOK,
				"fresh_until": now.Add(time.Hour).Format(time.RFC3339Nano),
				"data": map[string]any{
					"remaining_balance": 50.0,
				},
			},
		},
	}
	// Candidate metadata only needs the fresh balance, not console credentials
	// or the display-only fields from the complete wallet response.
	payload, err := json.Marshal(account)
	require.NoError(t, err)
	var decoded Account
	require.NoError(t, json.Unmarshal(payload, &decoded))
	require.True(t, tierflowBalanceProbeAllowsScheduling(&decoded, now))

	scheduler := &defaultOpenAIAccountScheduler{service: &OpenAIGatewayService{}}
	compatible, reason := scheduler.isAccountRequestCompatibleReason(context.Background(), &decoded, OpenAIAccountScheduleRequest{})
	require.True(t, compatible, reason)
	require.Empty(t, reason)
}

func TestTierflowUsageMissingCredentialsAndProbeBackoffDoNotQueryProvider(t *testing.T) {
	account := newTierflowBalanceTestAccount()
	delete(account.Credentials, "tierflow_cookie")
	svc := &AccountUsageService{}
	usage, err := svc.getTierflowUsage(context.Background(), account, false)
	require.NoError(t, err)
	require.Equal(t, "missing_credentials", usage.ErrorCode)
	require.Nil(t, usage.TierflowBalance)
	account.Credentials["tierflow_cookie"] = "test-session"
	account.Extra = map[string]any{UpstreamBillingProbeExtraKey: &UpstreamBillingProbeSnapshot{Status: UpstreamBillingProbeStatusFailed, HTTPStatus: http.StatusUnauthorized, NextProbeAt: time.Now().Add(time.Minute)}}
	usage, err = svc.getTierflowUsage(context.Background(), account, false)
	require.NoError(t, err)
	require.True(t, usage.NeedsReauth)
	require.Equal(t, "unauthenticated", usage.ErrorCode)
	require.Nil(t, usage.TierflowBalance)
}
