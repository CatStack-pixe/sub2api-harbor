package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const kimiBalanceBodyLimit int64 = 1 << 20

// KimiBalanceResult is the safe, administrator-facing representation of the
// Kimi Open Platform balance response.
type KimiBalanceResult struct {
	IsAvailable      bool    `json:"is_available"`
	AvailableBalance float64 `json:"available_balance"`
	VoucherBalance   float64 `json:"voucher_balance"`
	CashBalance      float64 `json:"cash_balance"`
	Currency         string  `json:"currency"`
	StatusCode       int     `json:"status_code,omitempty"`
	FetchedAt        int64   `json:"fetched_at"`
}

type kimiBalanceResponseWire struct {
	Code    json.RawMessage `json:"code"`
	Status  json.RawMessage `json:"status"`
	Message string          `json:"message"`
	Data    struct {
		AvailableBalance float64 `json:"available_balance"`
		VoucherBalance   float64 `json:"voucher_balance"`
		CashBalance      float64 `json:"cash_balance"`
	} `json:"data"`
}

func ParseKimiBalanceResponse(body []byte) (*KimiBalanceResult, error) {
	var wire kimiBalanceResponseWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode Kimi balance response: %w", err)
	}
	if !kimiBalanceResponseSucceeded(wire.Code, wire.Status) {
		message := strings.TrimSpace(wire.Message)
		if message == "" {
			message = "Kimi balance request was rejected"
		}
		return nil, infraerrors.Newf(http.StatusBadGateway, "KIMI_BALANCE_UPSTREAM_ERROR", "%s", message)
	}
	return &KimiBalanceResult{
		IsAvailable:      true,
		AvailableBalance: wire.Data.AvailableBalance,
		VoucherBalance:   wire.Data.VoucherBalance,
		CashBalance:      wire.Data.CashBalance,
	}, nil
}

func kimiBalanceResponseSucceeded(code, status json.RawMessage) bool {
	codeText := strings.TrimSpace(string(code))
	if unquoted, err := strconv.Unquote(codeText); err == nil {
		codeText = strings.TrimSpace(unquoted)
	}
	if codeText != "0" {
		return false
	}
	var success bool
	return json.Unmarshal(status, &success) == nil && success
}

// FetchKimiBalance queries the official account balance endpoint. It is
// intentionally read-only and never changes account scheduling state.
func (s *AccountTestService) FetchKimiBalance(ctx context.Context, accountID int64) (*KimiBalanceResult, error) {
	if s == nil || s.accountRepo == nil {
		return nil, fmt.Errorf("account test service is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil || !account.IsKimi() {
		return nil, fmt.Errorf("account is not a Kimi account")
	}
	if err := validateAccountCredentials(account.Platform, account.Type, account.Credentials); err != nil {
		return nil, err
	}
	if s.httpUpstream == nil {
		return nil, fmt.Errorf("upstream HTTP client is not configured")
	}

	baseURL := account.GetOpenAIBaseURL()
	if !isKimiBaseURL(baseURL) {
		return nil, fmt.Errorf("invalid Kimi base URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildKimiBalanceURL(baseURL), nil)
	if err != nil {
		return nil, fmt.Errorf("create Kimi balance request: %w", err)
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
		return nil, fmt.Errorf("Kimi balance request failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("Kimi balance request returned no response")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, kimiBalanceBodyLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read Kimi balance response: %w", err)
	}
	if int64(len(body)) > kimiBalanceBodyLimit {
		return nil, fmt.Errorf("Kimi balance response exceeds %d bytes", kimiBalanceBodyLimit)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, infraerrors.Newf(resp.StatusCode, "KIMI_BALANCE_UPSTREAM_ERROR", "Kimi balance request failed with HTTP %d", resp.StatusCode)
	}

	result, err := ParseKimiBalanceResponse(body)
	if err != nil {
		return nil, err
	}
	result.Currency = kimiBalanceCurrency(baseURL)
	result.StatusCode = resp.StatusCode
	result.FetchedAt = time.Now().Unix()
	return result, nil
}

func buildKimiBalanceURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/users/me/balance"
}

func kimiBalanceCurrency(baseURL string) string {
	if strings.TrimRight(strings.TrimSpace(baseURL), "/") == KimiInternationalBaseURL {
		return "USD"
	}
	return "CNY"
}
