//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestWalletErrorsForwardingMapsModelOnce(t *testing.T) {
	for _, tc := range []struct {
		name, model, nextModel, body, scope, reason string
		account                                     func() *Account
	}{
		{
			name: "cline pass", model: "cline-pass/glm-5.3", nextModel: "deepseek/deepseek-v4-flash",
			body: clinePass5hBody, scope: clinePassRateLimitKey, reason: clinePassLimitReason,
			account: func() *Account { return clineTestAccount(801) },
		},
		{
			name: "cline credits", model: "deepseek/deepseek-v4-flash", nextModel: "cline-pass/glm-5.3",
			body: clineSpendLimitBody, scope: clineCreditsRateLimitKey, reason: clineSpendLimitReason,
			account: func() *Account { return clineTestAccount(802) },
		},
		{
			name: "cline free", model: "cline-free/kimi-k3", nextModel: "deepseek/deepseek-v4-flash",
			body: clineFreeModelLimitBody, scope: "cline-free/kimi-k3", reason: clineFreeModelLimitReason,
			account: func() *Account { return clineTestAccount(803) },
		},
		{
			name: "command code", model: "glm-5", nextModel: "gpt-5.5",
			body: commandCodeSpendLimitBody, scope: "glm-5", reason: commandCodeSpendLimitReason,
			account: commandCodeUsageAccount,
		},
	} {
		for _, ingress := range routingMatrixIngresses() {
			t.Run(tc.name+"/"+ingress.name, func(t *testing.T) {
				account := tc.account()
				account.Schedulable = true
				account.Credentials["model_mapping"] = map[string]any{
					"public-alias": tc.model,
					tc.model:       tc.nextModel,
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(tc.body)),
				}}
				repo := &commandCodeRateLimitRepo{}
				cfg := rawChatCompletionsTestConfig()
				svc := &OpenAIGatewayService{
					cfg: cfg, httpUpstream: upstream, accountRepo: repo,
					rateLimitService: NewRateLimitService(repo, nil, cfg, nil, nil),
				}
				body := routingMatrixCase{ingress: ingress, model: "public-alias"}.body()

				err := ingress.forward(svc, adaptiveProtocolTestContext(ingress.path, body), account, body)

				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.Equal(t, http.StatusTooManyRequests, failover.StatusCode)
				require.Len(t, upstream.requests, 1)
				require.True(t, strings.HasSuffix(upstream.lastReq.URL.Path, "/chat/completions"), upstream.lastReq.URL.String())
				require.Equal(t, tc.model, gjson.GetBytes(upstream.lastBody, "model").String())
				require.Len(t, repo.modelLimits, 1)
				require.Contains(t, repo.modelLimits, tc.scope)
				require.NotContains(t, repo.modelLimits, tc.nextModel)
				require.True(t, strings.HasPrefix(repo.reasons[tc.scope], tc.reason), repo.reasons[tc.scope])
				require.Contains(t, account.modelRateLimitKeysForRequest(context.Background(), "public-alias"), tc.scope)
				if account.IsCline() {
					require.True(t, account.isModelRateLimitedWithContext(context.Background(), "public-alias"))
					for _, scope := range []string{clinePassRateLimitKey, clineCreditsRateLimitKey} {
						if scope != tc.scope {
							require.False(t, account.isRateLimitActiveForKey(scope), scope)
						}
					}
					require.False(t, account.isModelRateLimitedWithContext(context.Background(), "cline-free/other"))
				}
				require.Zero(t, repo.tempCalls)
				require.Zero(t, repo.setErrorCalls)
				require.Zero(t, repo.rateLimitedCalls)
				_, blocked := svc.openaiAccountRuntimeBlockUntil.Load(account.ID)
				require.False(t, blocked)
			})
		}
	}
}

func TestClineWalletWriteFailuresStayIsolatedThroughForwarding(t *testing.T) {
	for _, tc := range []struct {
		name, model, body, scope string
		status                   int
	}{
		{"credits spend", "deepseek/deepseek-v4-flash", clineSpendLimitBody, clineCreditsRateLimitKey, http.StatusTooManyRequests},
		{"pass window", "cline-pass/glm-5.3", clinePass5hBody, clinePassRateLimitKey, http.StatusTooManyRequests},
		{"pass entitlement", "cline-pass/glm-5.3", clineNotSubscribedBody, clinePassRateLimitKey, http.StatusForbidden},
		{"free model", "cline-free/kimi-k3", clineFreeModelLimitBody, "cline-free/kimi-k3", http.StatusTooManyRequests},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := clineTestAccount(804)
			repo := &clineModelLimitFailRepo{}
			cfg := rawChatCompletionsTestConfig()
			blocker := &runtimeBlockRecorder{}
			rateLimits := NewRateLimitService(repo, nil, cfg, nil, nil)
			rateLimits.SetAccountRuntimeBlocker(blocker)
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: tc.status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(tc.body)),
			}}
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, accountRepo: repo, rateLimitService: rateLimits}
			body := []byte(`{"model":"` + tc.model + `","messages":[{"role":"user","content":"hello"}],"stream":false}`)
			c := adaptiveProtocolTestContext("/v1/chat/completions", body)

			_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Equal(t, tc.status, failover.StatusCode)
			require.Equal(t, 1, repo.modelAttempts)
			require.Equal(t, tc.scope, repo.failedScope)
			require.True(t, account.isModelRateLimitedWithContext(context.Background(), tc.model))
			for _, model := range []string{"cline-pass/glm-5.3", "deepseek/deepseek-v4-flash", "cline-free/kimi-k3", "cline-free/other"} {
				require.Equal(t, model == tc.model, account.isModelRateLimitedWithContext(context.Background(), model), model)
			}
			require.Zero(t, repo.tempCalls)
			require.Zero(t, repo.setErrorCalls)
			require.Zero(t, repo.rateLimitedCalls)
			require.Empty(t, blocker.accounts)
			_, blocked := svc.openaiAccountRuntimeBlockUntil.Load(account.ID)
			require.False(t, blocked)
		})
	}
}

func probeClineWalletsForAudit(t *testing.T, repo *clineAccountRepo, usage commandCodeAlphaResponse, balance string) {
	t.Helper()
	upstream := newClineAccountUpstream(usage, balance)
	result, err := NewCNProviderQuotaService(repo, nil, upstream, clineUsageTestConfig()).QueryUsage(context.Background(), repo.accounts[0].ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, result.Persisted)
	require.NotNil(t, result.Balance)
	require.True(t, result.Balance.Success)
	require.Empty(t, repo.paused)
}

func TestClineUsageProbePreservesUnprobeableWalletCooldownsAcrossRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, model, body, scope string
		status                   int
	}{
		{"credits spend", "deepseek/deepseek-v4-flash", clineSpendLimitBody, clineCreditsRateLimitKey, http.StatusTooManyRequests},
		{"credits inference", "deepseek/deepseek-v4-flash", clineInferenceCapBody, clineCreditsRateLimitKey, http.StatusTooManyRequests},
		{"pass spend", "cline-pass/glm-5.3", clineSpendLimitBody, clinePassRateLimitKey, http.StatusTooManyRequests},
		{"pass inference", "cline-pass/glm-5.3", clineInferenceCapBody, clinePassRateLimitKey, http.StatusTooManyRequests},
		{"required plan", "cline-pass/glm-5.3", clineNotSubscribedBody, clinePassRateLimitKey, http.StatusForbidden},
		{"organization", "cline-pass/glm-5.3", clineOrgPassBody, clinePassRateLimitKey, http.StatusForbidden},
	} {
		for _, wait := range []time.Duration{5 * time.Minute, 6 * time.Hour} {
			for _, lowProbe := range []string{"exhausted window", "no subscription"} {
				t.Run(tc.name+"/"+wait.String()+"/"+lowProbe, func(t *testing.T) {
					repo := &clineAccountRepo{accounts: []Account{*clineTestAccount(805)}}
					account := &repo.accounts[0]
					shouldDisable := NewRateLimitService(repo, nil, clineUsageTestConfig(), nil, nil).HandleUpstreamError(
						context.Background(), account, tc.status, http.Header{}, []byte(tc.body), tc.model)
					require.False(t, shouldDisable)
					reason := account.modelRateLimitReason(tc.scope)
					require.NotEmpty(t, reason)
					now := time.Now().UTC().Truncate(time.Second)
					deadline := now.Add(wait)
					setAccountModelRateLimitSnapshot(account, tc.scope, deadline, reason, now)
					require.NoError(t, repo.SetModelRateLimit(context.Background(), account.ID, tc.scope, deadline, reason))
					original := account.Extra[modelRateLimitsKey].(map[string]any)[tc.scope]

					usage := commandCodeAlphaResponse{status: http.StatusNotFound, body: `{"success":false,"error":"no plan history found for user"}`}
					if lowProbe == "exhausted window" {
						usage = commandCodeAlphaResponse{status: http.StatusOK, body: `{"success":true,"data":{"limits":[{"type":"weekly","percentUsed":100,"resetsAt":"` +
							now.Add(3*time.Hour).Format(time.RFC3339) + `"}]}}`}
					}
					probeClineWalletsForAudit(t, repo, usage, `{"success":true,"data":{"balance":100000}}`)

					require.Equal(t, original, account.Extra[modelRateLimitsKey].(map[string]any)[tc.scope])
					require.Equal(t, reason, repo.reasons[account.ID][tc.scope])
					require.True(t, repo.modelLimits[account.ID][tc.scope].Equal(deadline))
					require.True(t, account.isModelRateLimitedWithContext(context.Background(), "cline-pass/glm-5.3"))
					require.True(t, account.isModelRateLimitedWithContext(context.Background(), "deepseek/deepseek-v4-flash"))
					require.False(t, account.isModelRateLimitedWithContext(context.Background(), "cline-free/kimi-k3"))

					probeClineWalletsForAudit(t, repo, commandCodeAlphaResponse{status: http.StatusOK, body: clineUsageLimitsBody},
						`{"success":true,"data":{"balance":5000000}}`)

					require.Equal(t, original, account.Extra[modelRateLimitsKey].(map[string]any)[tc.scope])
					require.Equal(t, reason, repo.reasons[account.ID][tc.scope])
					require.True(t, repo.modelLimits[account.ID][tc.scope].Equal(deadline))
					require.True(t, account.isModelRateLimitedWithContext(context.Background(), tc.model))
					otherScope := clinePassRateLimitKey
					if tc.scope == clinePassRateLimitKey {
						otherScope = clineCreditsRateLimitKey
					}
					require.False(t, account.isRateLimitActiveForKey(otherScope))
					require.False(t, repo.modelLimits[account.ID][otherScope].After(time.Now()))
					require.False(t, account.isModelRateLimitedWithContext(context.Background(), "cline-free/kimi-k3"))
				})
			}
		}
	}
}

func TestClineUsageProbeKeepsIndependentUnprobeableWallets(t *testing.T) {
	repo := &clineAccountRepo{accounts: []Account{*clineTestAccount(806)}}
	account := &repo.accounts[0]
	now := time.Now().UTC().Truncate(time.Second)
	for _, restriction := range []struct {
		scope, reason string
		wait          time.Duration
	}{
		{clineCreditsRateLimitKey, clineSpendLimitReason + ": organization budget", 30 * time.Minute},
		{clinePassRateLimitKey, clinePassUnavailableReason + ": required plan", 2 * time.Hour},
	} {
		deadline := now.Add(restriction.wait)
		setAccountModelRateLimitSnapshot(account, restriction.scope, deadline, restriction.reason, now)
		require.NoError(t, repo.SetModelRateLimit(context.Background(), account.ID, restriction.scope, deadline, restriction.reason))
	}
	credits := account.Extra[modelRateLimitsKey].(map[string]any)[clineCreditsRateLimitKey]
	pass := account.Extra[modelRateLimitsKey].(map[string]any)[clinePassRateLimitKey]

	for _, probe := range []struct {
		usage                             commandCodeAlphaResponse
		balance string
	}{
		{commandCodeAlphaResponse{status: http.StatusNotFound, body: `{"success":false,"error":"no plan history found for user"}`}, `{"success":true,"data":{"balance":0}}`},
		{commandCodeAlphaResponse{status: http.StatusOK, body: clineUsageLimitsBody}, `{"success":true,"data":{"balance":5000000}}`},
	} {
		probeClineWalletsForAudit(t, repo, probe.usage, probe.balance)
		require.Equal(t, credits, account.Extra[modelRateLimitsKey].(map[string]any)[clineCreditsRateLimitKey])
		require.Equal(t, pass, account.Extra[modelRateLimitsKey].(map[string]any)[clinePassRateLimitKey])
		require.True(t, repo.modelLimits[account.ID][clineCreditsRateLimitKey].Equal(now.Add(30*time.Minute)))
		require.True(t, repo.modelLimits[account.ID][clinePassRateLimitKey].Equal(now.Add(2*time.Hour)))
		require.Equal(t, clineSpendLimitReason+": organization budget", repo.reasons[account.ID][clineCreditsRateLimitKey])
		require.Equal(t, clinePassUnavailableReason+": required plan", repo.reasons[account.ID][clinePassRateLimitKey])
		require.True(t, account.isModelRateLimitedWithContext(context.Background(), "deepseek/deepseek-v4-flash"))
		require.True(t, account.isModelRateLimitedWithContext(context.Background(), "cline-pass/glm-5.3"))
		require.False(t, account.isModelRateLimitedWithContext(context.Background(), "cline-free/kimi-k3"))
	}
}

func TestClineUsageProbeReplacesExpiredUnprobeableCooldowns(t *testing.T) {
	for _, tc := range []struct {
		name, scope, oldReason, newReason string
		usage                             commandCodeAlphaResponse
	}{
		{
			name: "credits", scope: clineCreditsRateLimitKey, oldReason: clineSpendLimitReason, newReason: clineCreditsLowReason,
			usage: commandCodeAlphaResponse{status: http.StatusOK, body: clineUsageLimitsBody},
		},
		{
			name: "pass subscription", scope: clinePassRateLimitKey, oldReason: clinePassUnavailableReason, newReason: clinePassNoSubscriptionReason,
			usage: commandCodeAlphaResponse{status: http.StatusNotFound, body: `{"success":false,"error":"no plan history found for user"}`},
		},
		{
			name: "pass window", scope: clinePassRateLimitKey, oldReason: clineInferenceCapReason, newReason: clinePassLimitReason,
			usage: commandCodeAlphaResponse{status: http.StatusOK, body: `{"success":true,"data":{"limits":[{"type":"five_hour","percentUsed":100}]}}`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &clineAccountRepo{accounts: []Account{*clineTestAccount(807)}}
			account := &repo.accounts[0]
			now := time.Now()
			setAccountModelRateLimitSnapshot(account, tc.scope, now.Add(-time.Minute), tc.oldReason, now)
			balance := `{"success":true,"data":{"balance":5000000}}`
			if tc.scope == clineCreditsRateLimitKey {
				balance = `{"success":true,"data":{"balance":100000}}`
			}

			probeClineWalletsForAudit(t, repo, tc.usage, balance)

			require.True(t, strings.HasPrefix(account.modelRateLimitReason(tc.scope), tc.newReason))
			require.True(t, account.isRateLimitActiveForKey(tc.scope))
			require.True(t, repo.modelLimits[account.ID][tc.scope].After(time.Now()))

			probeClineWalletsForAudit(t, repo, commandCodeAlphaResponse{status: http.StatusOK, body: clineUsageLimitsBody},
				`{"success":true,"data":{"balance":5000000}}`)

			require.False(t, account.isRateLimitActiveForKey(tc.scope))
			require.False(t, repo.modelLimits[account.ID][tc.scope].After(time.Now()))
		})
	}
}
