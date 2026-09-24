package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

const (
	tierflowBalanceURL = "https://tierflow.cn/api/user/self"
	tierflowStatusURL = "https://tierflow.cn/api/status"
	tierflowBalanceBodyLimit = 1 << 20
	tierflowBalanceRequestTimeout = 20 * time.Second
)

// TierflowBalanceResult contains only observed wallet values. It excludes
// console credentials and the provider's unrelated dashboard billing limits.
type TierflowBalanceResult struct {
	IsAvailable bool `json:"is_available"`
	RemainingBalance float64 `json:"remaining_balance"`
	TotalUsage float64 `json:"total_usage"`
	Currency string `json:"currency"`
	QuotaPerUnit float64 `json:"quota_per_unit"`
	RequestCount int64 `json:"request_count"`
	StatusCode int `json:"status_code,omitempty"`
	FetchedAt int64 `json:"fetched_at"`
}

func tierflowQuotaNumber(raw json.RawMessage) (float64, error) {
	value := strings.TrimSpace(string(raw))
	if unquoted, err := strconv.Unquote(value); err == nil {
		value = strings.TrimSpace(unquoted)
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("Tierflow balance response contains an invalid quota value")
	}
	return number, nil
}

func ParseTierflowBalanceResponse(balanceBody, statusBody []byte) (*TierflowBalanceResult, error) {
	var balance struct {
		Success bool `json:"success"`
		Data struct {
			Quota json.RawMessage `json:"quota"`
			UsedQuota json.RawMessage `json:"used_quota"`
			RequestCount *int64 `json:"request_count"`
		} `json:"data"`
	}
	var status struct {
		Success bool `json:"success"`
		Data struct {
			QuotaPerUnit json.RawMessage `json:"quota_per_unit"`
		} `json:"data"`
	}
	if json.Unmarshal(balanceBody, &balance) != nil || json.Unmarshal(statusBody, &status) != nil {
		return nil, fmt.Errorf("Tierflow balance response is invalid")
	}
	if !balance.Success || !status.Success {
		return nil, fmt.Errorf("Tierflow balance request was rejected")
	}
	quota, quotaErr := tierflowQuotaNumber(balance.Data.Quota)
	usedQuota, usedErr := tierflowQuotaNumber(balance.Data.UsedQuota)
	quotaPerUnit, unitErr := tierflowQuotaNumber(status.Data.QuotaPerUnit)
	if quotaErr != nil || usedErr != nil || unitErr != nil || quotaPerUnit <= 0 || usedQuota < 0 || balance.Data.RequestCount == nil || *balance.Data.RequestCount < 0 {
		return nil, fmt.Errorf("Tierflow balance response has incomplete or invalid quota data")
	}
	remaining, used := quota/quotaPerUnit, usedQuota/quotaPerUnit
	if math.IsInf(remaining, 0) || math.IsInf(used, 0) {
		return nil, fmt.Errorf("Tierflow balance response contains an invalid quota value")
	}
	return &TierflowBalanceResult{
		IsAvailable: true,
		RemainingBalance: remaining,
		TotalUsage: used,
		Currency: "CNY",
		QuotaPerUnit: quotaPerUnit,
		RequestCount: *balance.Data.RequestCount,
	}, nil
}

// FetchTierflowBalance uses the fixed official console origin, disables
// redirects, and honors the account's proxy and TLS fingerprint settings.
func (s *AccountTestService) FetchTierflowBalance(ctx context.Context, accountID int64) (*TierflowBalanceResult, error) {
	if s == nil || s.accountRepo == nil || s.httpUpstream == nil {
		return nil, fmt.Errorf("Tierflow balance service is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil || !account.IsTierflow() || account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("account is not a Tierflow API key account")
	}
	session, userID, err := TierflowCookieCredentials(account.Credentials)
	if err != nil {
		return nil, err
	}
	proxyURL := ""
	if account.ProxyID != nil {
		if account.Proxy == nil || account.Proxy.ID != *account.ProxyID {
			return nil, fmt.Errorf("Tierflow balance account proxy is unavailable")
		}
		proxyURL = account.Proxy.URL()
	}
	var tlsProfile *tlsfingerprint.Profile
	if s.tlsFPProfileService != nil {
		tlsProfile = s.tlsFPProfileService.ResolveTLSProfile(account)
	}
	requestCtx, cancel := context.WithTimeout(WithHTTPUpstreamRedirectsDisabled(ctx), tierflowBalanceRequestTimeout)
	defer cancel()
	fetch := func(endpoint string, authenticated bool) ([]byte, int, error) {
		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("create Tierflow balance request failed")
		}
		req.Header.Set("Accept", "application/json")
		if authenticated {
			req.Header.Set("Cookie", "session="+session)
			req.Header.Set("TF-User", userID)
		}
		resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, tlsProfile)
		if err != nil {
			// Transport errors may contain URLs, proxy credentials or request
			// headers. Do not surface their text through the admin usage API.
			return nil, 0, fmt.Errorf("Tierflow balance request failed")
		}
		if resp == nil || resp.Body == nil {
			return nil, 0, fmt.Errorf("Tierflow balance request returned no response")
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil, resp.StatusCode, infraerrors.Newf(resp.StatusCode, "TIERFLOW_BALANCE_UPSTREAM_ERROR", "Tierflow balance request failed with HTTP %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, tierflowBalanceBodyLimit+1))
		if err != nil {
			return nil, resp.StatusCode, fmt.Errorf("read Tierflow balance response failed")
		}
		if len(body) > tierflowBalanceBodyLimit {
			return nil, resp.StatusCode, fmt.Errorf("Tierflow balance response exceeds %d bytes", tierflowBalanceBodyLimit)
		}
		return body, resp.StatusCode, nil
	}
	balanceBody, statusCode, err := fetch(tierflowBalanceURL, true)
	if err != nil {
		return nil, err
	}
	statusBody, _, err := fetch(tierflowStatusURL, false)
	if err != nil {
		return nil, err
	}
	result, err := ParseTierflowBalanceResponse(balanceBody, statusBody)
	if err != nil {
		return nil, err
	}
	result.StatusCode = statusCode
	result.FetchedAt = time.Now().Unix()
	return result, nil
}

func (s *AccountUsageService) getTierflowUsage(ctx context.Context, account *Account, force bool) (*UsageInfo, error) {
	usage := &UsageInfo{Source: "active"}
	if _, _, err := TierflowCookieCredentials(account.Credentials); err != nil {
		usage.ErrorCode = "missing_credentials"
		usage.Error = "Tierflow session Cookie and user ID are required for balance queries"
		return usage, nil
	}
	now := time.Now()
	snapshot := decodeUpstreamBillingProbeSnapshot(account.Extra)
	fresh := snapshot != nil && snapshot.Status == UpstreamBillingProbeStatusOK && snapshot.FreshUntil != nil && snapshot.FreshUntil.After(now)
	retryPending := snapshot != nil && snapshot.Status != UpstreamBillingProbeStatusOK && snapshot.NextProbeAt.After(now)
	if force || (!fresh && !retryPending) {
		if s.upstreamBillingProbeService == nil {
			usage.ErrorCode = "unavailable"
			usage.Error = "Tierflow balance service is not configured"
			return usage, nil
		}
		var err error
		snapshot, err = s.upstreamBillingProbeService.ProbeAccount(ctx, account.ID)
		if err != nil {
			usage.ErrorCode = "network_error"
			usage.Error = "Tierflow balance query failed"
			return usage, nil
		}
	}
	if snapshot == nil || snapshot.Status != UpstreamBillingProbeStatusOK {
		usage.ErrorCode = "balance_unavailable"
		usage.Error = "Tierflow balance query failed"
		if snapshot != nil && (snapshot.HTTPStatus == http.StatusUnauthorized || snapshot.HTTPStatus == http.StatusForbidden) {
			usage.NeedsReauth = true
			usage.ErrorCode = "unauthenticated"
		}
		return usage, nil
	}
	body, err := json.Marshal(snapshot.Data)
	if err != nil {
		return nil, fmt.Errorf("Tierflow balance snapshot is invalid")
	}
	var result TierflowBalanceResult
	if json.Unmarshal(body, &result) != nil || !result.IsAvailable || result.QuotaPerUnit <= 0 {
		return nil, fmt.Errorf("Tierflow balance snapshot is invalid")
	}
	result.StatusCode = snapshot.HTTPStatus
	usage.TierflowBalance = &result
	usage.UpdatedAt = snapshot.ReceivedAt
	return usage, nil
}
