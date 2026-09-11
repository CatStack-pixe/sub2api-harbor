package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"golang.org/x/sync/singleflight"
)

const (
	senseNovaQuotaURL              = "https://platform.sensenova.cn/lite/console/v1/tokenplan/pool-usage"
	senseNovaQuotaTimeout          = 15 * time.Second
	senseNovaQuotaMaxBytes         = 256 * 1024
	SenseNovaQuotaSnapshotExtraKey = "sensenova_quota_snapshot"
)

// SenseNovaQuotaPlan identifies the Token Plan returned by the quota API.
type SenseNovaQuotaPlan struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

// SenseNovaQuotaWindow is one independent Token Plan rolling window.
type SenseNovaQuotaWindow struct {
	Limit     float64 `json:"limit"`
	Used      float64 `json:"used"`
	Remaining float64 `json:"remaining"`
	ResetAt   string  `json:"reset_at,omitempty"`
}

// SenseNovaQuotaPool preserves one upstream pool without aggregating it with
// other pools. Some plans expose only grant metadata, so both windows remain
// optional.
type SenseNovaQuotaPool struct {
	ID                 string                `json:"id,omitempty"`
	Name               string                `json:"name,omitempty"`
	PoolType           string                `json:"pool_type,omitempty"`
	ModelIDs           []string              `json:"model_ids,omitempty"`
	Window5h           *SenseNovaQuotaWindow `json:"window_5h,omitempty"`
	Window7d           *SenseNovaQuotaWindow `json:"window_7d,omitempty"`
	GrantBalance       float64               `json:"grant_balance,omitempty"`
	NearestGrantExpiry string                `json:"nearest_grant_expiry,omitempty"`
}

// SenseNovaQuotaSnapshot is persisted in account.extra for the account page
// and reused by the channel monitor quota fetcher.
type SenseNovaQuotaSnapshot struct {
	Plan      SenseNovaQuotaPlan   `json:"plan"`
	Pools     []SenseNovaQuotaPool `json:"pools"`
	FetchedAt string               `json:"fetched_at"`
}

// SenseNovaQuotaProbeResult is the admin API response for a native quota
// query. The API key used for gateway traffic is intentionally not included;
// this endpoint is authenticated with the separate access_token credential.
type SenseNovaQuotaProbeResult struct {
	Provider        string               `json:"provider"`
	Source          string               `json:"source"`
	Success         bool                 `json:"success"`
	CredentialValid bool                 `json:"credential_valid"`
	Plan            SenseNovaQuotaPlan   `json:"plan,omitempty"`
	Pools           []SenseNovaQuotaPool `json:"pools,omitempty"`
	StatusCode      int                  `json:"status_code,omitempty"`
	FetchedAt       int64                `json:"fetched_at"`
	Persisted       bool                 `json:"persisted"`
	Error           string               `json:"error,omitempty"`
}

// SenseNovaQuotaService queries the native Token Plan pool usage endpoint.
type SenseNovaQuotaService struct {
	accountRepo  AccountRepository
	proxyRepo    ProxyRepository
	httpUpstream HTTPUpstream
	cfg          *config.Config
	flight       singleflight.Group
}

func NewSenseNovaQuotaService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
) *SenseNovaQuotaService {
	return &SenseNovaQuotaService{
		accountRepo:  accountRepo,
		proxyRepo:    proxyRepo,
		httpUpstream: httpUpstream,
		cfg:          cfg,
	}
}

// QueryUsage queries and persists the quota snapshot for an account ID.
func (s *SenseNovaQuotaService) QueryUsage(ctx context.Context, accountID int64) (*SenseNovaQuotaProbeResult, error) {
	if s == nil || s.accountRepo == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "SENSENOVA_QUOTA_NOT_CONFIGURED", "sensenova quota service is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusNotFound, "SENSENOVA_QUOTA_ACCOUNT_NOT_FOUND", "account not found: %v", err)
	}
	if err := validateSenseNovaQuotaAccount(account); err != nil {
		return nil, err
	}
	return s.QueryUsageForAccount(ctx, account)
}

// QueryUsageForAccount queries an already loaded account so channel monitoring
// can reuse its account lookup and proxy relation.
func (s *SenseNovaQuotaService) QueryUsageForAccount(ctx context.Context, account *Account) (*SenseNovaQuotaProbeResult, error) {
	if s == nil || s.accountRepo == nil || s.httpUpstream == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "SENSENOVA_QUOTA_NOT_CONFIGURED", "sensenova quota service is not configured")
	}
	if err := validateSenseNovaQuotaAccount(account); err != nil {
		return nil, err
	}

	key := "sensenova_quota:" + strconv.FormatInt(account.ID, 10)
	resultCh := s.flight.DoChan(key, func() (any, error) {
		probeCtx, cancel := context.WithTimeout(context.Background(), senseNovaQuotaTimeout+5*time.Second)
		defer cancel()
		return s.queryUsageForAccount(probeCtx, account)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.Err != nil {
			return nil, result.Err
		}
		probe, ok := result.Val.(*SenseNovaQuotaProbeResult)
		if !ok || probe == nil {
			return nil, infraerrors.New(http.StatusInternalServerError, "SENSENOVA_QUOTA_RESULT_INVALID", "invalid sensenova quota probe result")
		}
		cloned := *probe
		if probe.Pools != nil {
			cloned.Pools = append([]SenseNovaQuotaPool(nil), probe.Pools...)
		}
		return &cloned, nil
	}
}

func (s *SenseNovaQuotaService) queryUsageForAccount(ctx context.Context, account *Account) (*SenseNovaQuotaProbeResult, error) {
	accessToken := strings.TrimSpace(account.GetCredential("access_token"))
	if accessToken == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "SENSENOVA_QUOTA_NO_ACCESS_TOKEN", "account access_token is empty")
	}

	targetURL, err := cnValidateProbeURL(s.cfg, senseNovaQuotaURL)
	if err != nil {
		return nil, infraerrors.New(http.StatusForbidden, "SENSENOVA_QUOTA_URL_REJECTED", err.Error())
	}

	callCtx, cancel := context.WithTimeout(ctx, senseNovaQuotaTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "SENSENOVA_QUOTA_REQUEST_BUILD_FAILED", "build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(req.Header)

	resp, err := s.httpUpstream.Do(req, s.resolveProxyURL(ctx, account), account.ID, maxInt(account.Concurrency, 1))
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "SENSENOVA_QUOTA_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	if resp == nil {
		return nil, infraerrors.New(http.StatusBadGateway, "SENSENOVA_QUOTA_REQUEST_FAILED", "upstream returned an empty response")
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, senseNovaQuotaMaxBytes))

	now := time.Now().UTC()
	result := &SenseNovaQuotaProbeResult{
		Provider:   PlatformSenseNova,
		Source:     "sensenova_quota",
		StatusCode: resp.StatusCode,
		FetchedAt:  now.Unix(),
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		result.Error = fmt.Sprintf("Authentication failed (HTTP %d)", resp.StatusCode)
		return result, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = fmt.Sprintf("API error (HTTP %d): %s", resp.StatusCode, truncate(strings.TrimSpace(string(body)), 240))
		return result, nil
	}
	result.CredentialValid = true

	snapshot, err := parseSenseNovaQuotaSnapshot(body, now)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	result.Plan = snapshot.Plan
	result.Pools = snapshot.Pools
	result.Success = true
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
		SenseNovaQuotaSnapshotExtraKey: snapshot,
	}); err != nil {
		slog.Warn("sensenova_quota_persist_failed", "account_id", account.ID, "error", err)
	} else {
		result.Persisted = true
	}
	return result, nil
}

func (s *SenseNovaQuotaService) resolveProxyURL(ctx context.Context, account *Account) string {
	if account == nil || account.ProxyID == nil {
		return ""
	}
	if account.Proxy != nil {
		return account.Proxy.URL()
	}
	if s != nil && s.proxyRepo != nil {
		if proxy, err := s.proxyRepo.GetByID(ctx, *account.ProxyID); err == nil && proxy != nil {
			account.Proxy = proxy
			return proxy.URL()
		}
	}
	return ""
}

func validateSenseNovaQuotaAccount(account *Account) error {
	if account == nil {
		return infraerrors.New(http.StatusNotFound, "SENSENOVA_QUOTA_ACCOUNT_NOT_FOUND", "account not found")
	}
	if !account.IsSenseNova() {
		return infraerrors.New(http.StatusBadRequest, "SENSENOVA_QUOTA_INVALID_PLATFORM", "account is not a sensenova account")
	}
	return nil
}

type senseNovaQuotaResponse struct {
	Plan  SenseNovaQuotaPlan           `json:"plan"`
	Pools []senseNovaQuotaPoolResponse `json:"pools"`
}

type senseNovaQuotaPoolResponse struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	PoolType           string          `json:"pool_type"`
	ModelIDs           []string        `json:"model_ids"`
	Window5h           json.RawMessage `json:"window_5h"`
	Window7d           json.RawMessage `json:"window_7d"`
	GrantBalance       json.RawMessage `json:"grant_balance"`
	NearestGrantExpiry json.RawMessage `json:"nearest_grant_expiry"`
}

type senseNovaQuotaWindowResponse struct {
	Limit     json.RawMessage `json:"limit"`
	Used      json.RawMessage `json:"used"`
	Remaining json.RawMessage `json:"remaining"`
	ResetAt   json.RawMessage `json:"reset_at"`
}

func parseSenseNovaQuotaSnapshot(body []byte, now time.Time) (*SenseNovaQuotaSnapshot, error) {
	var payload senseNovaQuotaResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid sensenova quota response: %w", err)
	}
	if len(payload.Pools) == 0 {
		return nil, fmt.Errorf("invalid sensenova quota response: no quota pools")
	}

	pools := make([]SenseNovaQuotaPool, 0, len(payload.Pools))
	for _, rawPool := range payload.Pools {
		pool := SenseNovaQuotaPool{
			ID:       strings.TrimSpace(rawPool.ID),
			Name:     strings.TrimSpace(rawPool.Name),
			PoolType: strings.TrimSpace(rawPool.PoolType),
			ModelIDs: append([]string(nil), rawPool.ModelIDs...),
		}
		if rawPool.GrantBalance != nil {
			pool.GrantBalance, _ = senseNovaJSONNumber(rawPool.GrantBalance)
		}
		if rawPool.NearestGrantExpiry != nil {
			pool.NearestGrantExpiry = senseNovaJSONTime(rawPool.NearestGrantExpiry)
		}

		var err error
		pool.Window5h, err = parseSenseNovaQuotaWindow(rawPool.Window5h, pool.Name, "window_5h")
		if err != nil {
			return nil, err
		}
		pool.Window7d, err = parseSenseNovaQuotaWindow(rawPool.Window7d, pool.Name, "window_7d")
		if err != nil {
			return nil, err
		}
		pools = append(pools, pool)
	}

	return &SenseNovaQuotaSnapshot{
		Plan:      payload.Plan,
		Pools:     pools,
		FetchedAt: now.UTC().Format(time.RFC3339),
	}, nil
}

func parseSenseNovaQuotaWindow(raw json.RawMessage, poolName, windowName string) (*SenseNovaQuotaWindow, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var payload senseNovaQuotaWindowResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("invalid sensenova %s for pool %q: %w", windowName, poolName, err)
	}
	limit, ok := senseNovaJSONNumber(payload.Limit)
	if !ok || limit <= 0 {
		return nil, fmt.Errorf("invalid sensenova %s for pool %q: limit is missing or non-positive", windowName, poolName)
	}
	used, hasUsed := senseNovaJSONNumber(payload.Used)
	remaining, hasRemaining := senseNovaJSONNumber(payload.Remaining)
	switch {
	case !hasUsed && !hasRemaining:
		used, remaining = 0, limit
	case !hasUsed:
		used = limit - remaining
		if used < 0 {
			used = 0
		}
	case !hasRemaining:
		remaining = limit - used
		if remaining < 0 {
			remaining = 0
		}
	}
	if used < 0 {
		used = 0
	}
	if remaining < 0 {
		remaining = 0
	}
	return &SenseNovaQuotaWindow{
		Limit:     limit,
		Used:      used,
		Remaining: remaining,
		ResetAt:   senseNovaJSONTime(payload.ResetAt),
	}, nil
}

func senseNovaJSONNumber(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return 0, false
	}
	return cnParseF64(value)
}

func senseNovaJSONTime(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return ""
	}
	if text, ok := value.(string); ok {
		if n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64); err == nil {
			return cnMillisToRFC3339(n)
		}
	}
	return cnNormalizeResetTime(value)
}
