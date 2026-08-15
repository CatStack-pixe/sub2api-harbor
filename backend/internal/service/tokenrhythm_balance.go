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

const (
	tokenRhythmBalanceURL       = "https://tokenrhythm.studio/api/usage-summary"
	tokenRhythmBalanceBodyLimit = 1 << 20
)

// TokenRhythmBalanceResult is a safe, administrator-facing view of the
// provider usage summary. It intentionally never contains session cookies.
type TokenRhythmBalanceResult struct {
	IsAvailable         bool    `json:"is_available"`
	BalanceCNY          float64 `json:"balance_cny"`
	AvailableBalanceCNY float64 `json:"available_balance_cny"`
	FrozenBalanceCNY    float64 `json:"frozen_balance_cny"`
	ExpiringBalanceCNY  float64 `json:"expiring_balance_cny"`
	CostCNY             float64 `json:"cost_cny"`
	Currency            string  `json:"currency"`
	NextExpiryAt        string  `json:"next_expiry_at,omitempty"`
	Calls               int64   `json:"calls"`
	SuccessCalls        int64   `json:"success_calls"`
	ErrorCalls          int64   `json:"error_calls"`
	AbortedCalls        int64   `json:"aborted_calls"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	StatusCode          int     `json:"status_code,omitempty"`
	FetchedAt           int64   `json:"fetched_at"`
}

// tokenRhythmAmount accepts the number and quoted-number forms returned by
// the usage endpoint. The provider currently returns quoted amounts in live
// responses even though its documented example uses JSON numbers.
type tokenRhythmAmount float64

func (amount *tokenRhythmAmount) UnmarshalJSON(raw []byte) error {
	value := strings.TrimSpace(string(raw))
	if value == "null" {
		*amount = 0
		return nil
	}
	if unquoted, err := strconv.Unquote(value); err == nil {
		value = strings.TrimSpace(unquoted)
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("parse TokenRhythm amount %q: %w", value, err)
	}
	*amount = tokenRhythmAmount(parsed)
	return nil
}

type tokenRhythmBalanceWire struct {
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
	Data    struct {
		Calls               int64   `json:"calls"`
		SuccessCalls        int64   `json:"successCalls"`
		ErrorCalls          int64   `json:"errorCalls"`
		AbortedCalls        int64   `json:"abortedCalls"`
		InputTokens         int64   `json:"inputTokens"`
		OutputTokens        int64   `json:"outputTokens"`
		CostCNY             tokenRhythmAmount `json:"costCny"`
		BalanceCNY          tokenRhythmAmount `json:"balanceCny"`
		FrozenBalanceCNY    tokenRhythmAmount `json:"frozenBalanceCny"`
		AvailableBalanceCNY tokenRhythmAmount `json:"availableBalanceCny"`
		ExpiringBalanceCNY  tokenRhythmAmount `json:"expiringBalanceCny"`
		NextExpiryAt        string  `json:"nextExpiryAt"`
		Currency            string  `json:"currency"`
	} `json:"data"`
}

func ParseTokenRhythmBalanceResponse(body []byte) (*TokenRhythmBalanceResult, error) {
	var wire tokenRhythmBalanceWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode TokenRhythm balance response: %w", err)
	}
	if tokenRhythmBalanceCode(wire.Code) != "0" {
		message := strings.TrimSpace(wire.Message)
		if message == "" {
			message = "TokenRhythm balance request was rejected"
		}
		return nil, tokenRhythmBalanceResponseError(wire.Code, message)
	}

	return &TokenRhythmBalanceResult{
		IsAvailable:         true,
		BalanceCNY:          float64(wire.Data.BalanceCNY),
		AvailableBalanceCNY: float64(wire.Data.AvailableBalanceCNY),
		FrozenBalanceCNY:    float64(wire.Data.FrozenBalanceCNY),
		ExpiringBalanceCNY:  float64(wire.Data.ExpiringBalanceCNY),
		CostCNY:             float64(wire.Data.CostCNY),
		Currency:            strings.TrimSpace(wire.Data.Currency),
		NextExpiryAt:        strings.TrimSpace(wire.Data.NextExpiryAt),
		Calls:               wire.Data.Calls,
		SuccessCalls:        wire.Data.SuccessCalls,
		ErrorCalls:          wire.Data.ErrorCalls,
		AbortedCalls:        wire.Data.AbortedCalls,
		InputTokens:         wire.Data.InputTokens,
		OutputTokens:        wire.Data.OutputTokens,
	}, nil
}

func tokenRhythmBalanceCode(raw json.RawMessage) string {
	value := strings.TrimSpace(string(raw))
	if unquoted, err := strconv.Unquote(value); err == nil {
		return strings.TrimSpace(unquoted)
	}
	return value
}

func tokenRhythmBalanceResponseError(code json.RawMessage, message string) error {
	status := http.StatusBadGateway
	if strings.EqualFold(tokenRhythmBalanceCode(code), "UNAUTHORIZED") {
		status = http.StatusUnauthorized
	}
	return infraerrors.Newf(status, "TOKENRHYTHM_BALANCE_UPSTREAM_ERROR", "%s", message)
}

// FetchTokenRhythmBalance queries only the account-level usage endpoint. It
// does not alter account status or scheduling state.
func (s *AccountTestService) FetchTokenRhythmBalance(ctx context.Context, accountID int64) (*TokenRhythmBalanceResult, error) {
	if s == nil || s.accountRepo == nil {
		return nil, fmt.Errorf("account test service is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil || !account.IsTokenRhythm() {
		return nil, fmt.Errorf("account is not a TokenRhythm account")
	}
	if err := validateAccountCredentials(account.Platform, account.Type, account.Credentials); err != nil {
		return nil, err
	}
	if s.httpUpstream == nil {
		return nil, fmt.Errorf("upstream HTTP client is not configured")
	}
	session, csrf, err := TokenRhythmCookieCredentials(account.Credentials)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenRhythmBalanceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create TokenRhythm balance request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", tokenRhythmSessionKey+"="+session+"; "+tokenRhythmCSRFKey+"="+csrf)

	proxyURL := upstreamModelsProxyURL(account)
	var resp *http.Response
	if s.tlsFPProfileService == nil {
		resp, err = s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, nil)
	} else {
		resp, err = s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	}
	if err != nil {
		return nil, fmt.Errorf("TokenRhythm balance request failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("TokenRhythm balance request returned no response")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, tokenRhythmBalanceBodyLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read TokenRhythm balance response: %w", err)
	}
	if len(body) > tokenRhythmBalanceBodyLimit {
		return nil, fmt.Errorf("TokenRhythm balance response exceeds %d bytes", tokenRhythmBalanceBodyLimit)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, infraerrors.Newf(resp.StatusCode, "TOKENRHYTHM_BALANCE_UPSTREAM_ERROR", "TokenRhythm balance request failed with HTTP %d", resp.StatusCode)
	}

	result, err := ParseTokenRhythmBalanceResponse(body)
	if err != nil {
		return nil, err
	}
	result.StatusCode = resp.StatusCode
	result.FetchedAt = time.Now().Unix()
	return result, nil
}
