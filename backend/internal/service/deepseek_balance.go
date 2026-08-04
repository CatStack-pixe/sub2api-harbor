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
)

const deepSeekBalanceBodyLimit int64 = 1 << 20

type DeepSeekBalanceInfo struct {
	Currency        string `json:"currency"`
	TotalBalance    string `json:"total_balance"`
	GrantedBalance  string `json:"granted_balance"`
	ToppedUpBalance string `json:"topped_up_balance"`
}

type DeepSeekBalanceResult struct {
	IsAvailable  bool                   `json:"is_available"`
	BalanceInfos []DeepSeekBalanceInfo `json:"balance_infos"`
	StatusCode   int                    `json:"status_code,omitempty"`
	FetchedAt    int64                  `json:"fetched_at"`
}

type deepSeekBalanceInfoWire struct {
	Currency        string          `json:"currency"`
	TotalBalance    json.RawMessage `json:"total_balance"`
	GrantedBalance  json.RawMessage `json:"granted_balance"`
	ToppedUpBalance json.RawMessage `json:"topped_up_balance"`
}

type deepSeekBalanceResponseWire struct {
	IsAvailable  bool                       `json:"is_available"`
	BalanceInfos []deepSeekBalanceInfoWire `json:"balance_infos"`
}

func ParseDeepSeekBalanceResponse(body []byte) (*DeepSeekBalanceResult, error) {
	var wire deepSeekBalanceResponseWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode DeepSeek balance response: %w", err)
	}

	result := &DeepSeekBalanceResult{
		IsAvailable:  wire.IsAvailable,
		BalanceInfos: make([]DeepSeekBalanceInfo, 0, len(wire.BalanceInfos)),
	}
	for _, info := range wire.BalanceInfos {
		total, err := decodeDeepSeekBalanceAmount(info.TotalBalance)
		if err != nil {
			return nil, fmt.Errorf("decode total_balance for %s: %w", info.Currency, err)
		}
		granted, err := decodeDeepSeekBalanceAmount(info.GrantedBalance)
		if err != nil {
			return nil, fmt.Errorf("decode granted_balance for %s: %w", info.Currency, err)
		}
		toppedUp, err := decodeDeepSeekBalanceAmount(info.ToppedUpBalance)
		if err != nil {
			return nil, fmt.Errorf("decode topped_up_balance for %s: %w", info.Currency, err)
		}
		result.BalanceInfos = append(result.BalanceInfos, DeepSeekBalanceInfo{
			Currency:        strings.TrimSpace(info.Currency),
			TotalBalance:    total,
			GrantedBalance:  granted,
			ToppedUpBalance: toppedUp,
		})
	}
	return result, nil
}

func decodeDeepSeekBalanceAmount(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return validateDeepSeekBalanceAmount(text)
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return "", fmt.Errorf("invalid decimal value %s", string(raw))
	}
	return validateDeepSeekBalanceAmount(number.String())
}

func validateDeepSeekBalanceAmount(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return "", fmt.Errorf("invalid decimal value %q", raw)
	}
	return value, nil
}

func (s *AccountTestService) FetchDeepSeekBalance(ctx context.Context, accountID int64) (*DeepSeekBalanceResult, error) {
	if s == nil || s.accountRepo == nil {
		return nil, fmt.Errorf("account test service is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil || !account.IsDeepSeek() {
		return nil, fmt.Errorf("account is not a DeepSeek account")
	}
	if err := validateAccountCredentials(account.Platform, account.Type, account.Credentials); err != nil {
		return nil, err
	}
	if s.httpUpstream == nil {
		return nil, fmt.Errorf("upstream HTTP client is not configured")
	}

	baseURL, err := s.validateUpstreamBaseURL(account.GetOpenAIBaseURL())
	if err != nil {
		return nil, fmt.Errorf("invalid DeepSeek base URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildDeepSeekBalanceURL(baseURL), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid DeepSeek balance URL: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(account.GetOpenAIApiKey()))
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := upstreamModelsProxyURL(account)
	var resp *http.Response
	if s.tlsFPProfileService == nil {
		resp, err = s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, nil)
	} else {
		resp, err = s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	}
	if err != nil {
		return nil, fmt.Errorf("DeepSeek balance request failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("DeepSeek balance request returned no response")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, deepSeekBalanceBodyLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read DeepSeek balance response: %w", err)
	}
	if int64(len(body)) > deepSeekBalanceBodyLimit {
		return nil, fmt.Errorf("DeepSeek balance response exceeds %d bytes", deepSeekBalanceBodyLimit)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, infraerrors.Newf(resp.StatusCode, "DEEPSEEK_BALANCE_UPSTREAM_ERROR", "DeepSeek balance request failed with HTTP %d", resp.StatusCode)
	}

	result, err := ParseDeepSeekBalanceResponse(body)
	if err != nil {
		return nil, err
	}
	result.StatusCode = resp.StatusCode
	result.FetchedAt = time.Now().Unix()
	return result, nil
}

func buildDeepSeekBalanceURL(base string) string {
	normalized := strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(normalized, "/user/balance") {
		return normalized
	}
	return normalized + "/user/balance"
}
