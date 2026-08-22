package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	tokenRhythmReferralURL           = "https://tokenrhythm.studio/api/referrals/me"
	tokenRhythmInviteBaseURL         = "https://tokenrhythm.studio/i/"
	tokenRhythmSessionInputLimit     = 4096
	tokenRhythmSessionBodyLimit      = 1 << 20
	tokenRhythmSessionTimeout        = 15 * time.Second
	tokenRhythmSessionProbeUserAgent = "Mozilla/5.0 (compatible; Sub2API TokenRhythm session resolver)"
	tokenRhythmAPIKeyURL             = "https://tokenrhythm.studio/api/api-keys"
	tokenRhythmAPIKeyNameLimit       = 20
	tokenRhythmAPIKeyBodyLimit       = 1 << 20
)

// TokenRhythmSessionResult is returned only by the administrator session
// resolver. TokenRhythmCookie is intentionally limited to the two values that
// the account creation path already sanitizes and stores.
type TokenRhythmSessionResult struct {
	TokenRhythmCookie   string `json:"tokenrhythm_cookie"`
	ReferralCode        string `json:"referral_code"`
	ReferralLink        string `json:"referral_link"`
	Eligible            bool   `json:"eligible"`
	PublicEnabled       bool   `json:"public_enabled"`
	RegistrationAllowed bool   `json:"registration_allowed"`
}

// TokenRhythmAPIKeyResult is returned by the administrator key-management
// action. The sess value is deliberately never included in the result.
type TokenRhythmAPIKeyResult struct {
	APIKey             string `json:"api_key"`
	TokenRhythmCookie  string `json:"tokenrhythm_cookie"`
	Name               string `json:"name"`
}

type tokenRhythmReferralWire struct {
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
	Data    struct {
		Code                string `json:"code"`
		ReferralCode        string `json:"referralCode"`
		Eligible            bool   `json:"eligible"`
		CanInvite           bool   `json:"canInvite"`
		PublicEnabled       bool   `json:"publicEnabled"`
		RegistrationAllowed bool   `json:"registrationAllowed"`
	} `json:"data"`
}

type tokenRhythmAPIKeyWire struct {
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
	Key     string          `json:"key"`
	APIKey  string          `json:"api_key"`
	Data    struct {
		Key    string `json:"key"`
		APIKey string `json:"api_key"`
		Name   string `json:"name"`
	} `json:"data"`
}

// ResolveTokenRhythmSession validates an administrator-supplied session key,
// resolves the provider CSRF cookie, and returns the current referral link.
// It does not persist credentials or mutate account state.
func (s *AccountTestService) ResolveTokenRhythmSession(ctx context.Context, sessionKey, proxyURL string) (*TokenRhythmSessionResult, error) {
	if s == nil || s.httpUpstream == nil {
		return nil, fmt.Errorf("upstream HTTP client is not configured")
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if len(sessionKey) == 0 || len(sessionKey) > tokenRhythmSessionInputLimit || !strings.HasPrefix(sessionKey, "sess_") || !isValidTokenRhythmCookieValue(sessionKey) || containsControlCharacter(sessionKey) {
		return nil, infraerrors.BadRequest("TOKENRHYTHM_SESSION_INVALID", "TokenRhythm sess is invalid")
	}

	requestCtx, cancel := context.WithTimeout(WithHTTPUpstreamRedirectsDisabled(ctx), tokenRhythmSessionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, tokenRhythmReferralURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create TokenRhythm session request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", tokenRhythmSessionKey+"="+sessionKey)
	req.Header.Set("Referer", "https://tokenrhythm.studio/")
	req.Header.Set("User-Agent", tokenRhythmSessionProbeUserAgent)

	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, 0, 1, nil)
	if err != nil {
		return nil, fmt.Errorf("TokenRhythm session request failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("TokenRhythm session request returned no response")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, tokenRhythmSessionBodyLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read TokenRhythm session response: %w", err)
	}
	if len(body) > tokenRhythmSessionBodyLimit {
		return nil, infraerrors.Newf(http.StatusBadGateway, "TOKENRHYTHM_SESSION_UPSTREAM_ERROR", "TokenRhythm session response exceeds %d bytes", tokenRhythmSessionBodyLimit)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, infraerrors.Unauthorized("TOKENRHYTHM_SESSION_INVALID", "TokenRhythm sess is invalid or expired")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		status := resp.StatusCode
		if status >= http.StatusMultipleChoices && status < http.StatusBadRequest {
			status = http.StatusBadGateway
		}
		return nil, infraerrors.Newf(status, "TOKENRHYTHM_SESSION_UPSTREAM_ERROR", "TokenRhythm session request failed with HTTP %d", resp.StatusCode)
	}

	resolvedSession := sessionKey
	csrf := ""
	for _, cookie := range resp.Cookies() {
		if cookie == nil || !isValidTokenRhythmCookieValue(cookie.Value) || containsControlCharacter(cookie.Value) {
			continue
		}
		switch cookie.Name {
		case tokenRhythmSessionKey:
			resolvedSession = strings.TrimSpace(cookie.Value)
		case tokenRhythmCSRFKey:
			csrf = strings.TrimSpace(cookie.Value)
		}
	}
	if csrf == "" {
		return nil, infraerrors.New(http.StatusBadGateway, "TOKENRHYTHM_SESSION_COOKIE_MISSING", "TokenRhythm did not return the required CSRF cookie")
	}

	var wire tokenRhythmReferralWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "TOKENRHYTHM_SESSION_RESPONSE_INVALID", "TokenRhythm returned an invalid referral response")
	}
	if tokenRhythmBalanceCode(wire.Code) != "0" {
		if strings.EqualFold(tokenRhythmBalanceCode(wire.Code), "UNAUTHORIZED") {
			return nil, infraerrors.Unauthorized("TOKENRHYTHM_SESSION_INVALID", "TokenRhythm sess is invalid or expired")
		}
		return nil, infraerrors.New(http.StatusBadGateway, "TOKENRHYTHM_SESSION_UPSTREAM_ERROR", "TokenRhythm rejected the referral request")
	}
	referralCode := strings.TrimSpace(wire.Data.Code)
	if referralCode == "" {
		referralCode = strings.TrimSpace(wire.Data.ReferralCode)
	}
	if referralCode == "" || len(referralCode) > 256 || containsControlCharacter(referralCode) {
		return nil, infraerrors.New(http.StatusBadGateway, "TOKENRHYTHM_REFERRAL_CODE_MISSING", "TokenRhythm did not return a valid referral code")
	}

	return &TokenRhythmSessionResult{
		TokenRhythmCookie:   tokenRhythmSessionKey + "=" + resolvedSession + "; " + tokenRhythmCSRFKey + "=" + csrf,
		ReferralCode:        referralCode,
		ReferralLink:        tokenRhythmInviteBaseURL + url.PathEscape(referralCode),
		Eligible:            wire.Data.Eligible || wire.Data.CanInvite,
		PublicEnabled:       wire.Data.PublicEnabled,
		RegistrationAllowed: wire.Data.RegistrationAllowed,
	}, nil
}

// CreateTokenRhythmAPIKey validates sess through the provider session
// endpoint and creates one provider API key using the same server-side proxy
// and TLS path as the rest of the TokenRhythm integration. The sess is never
// persisted or returned.
func (s *AccountTestService) CreateTokenRhythmAPIKey(ctx context.Context, sessionKey, name, proxyURL string) (*TokenRhythmAPIKeyResult, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > tokenRhythmAPIKeyNameLimit || containsControlCharacter(name) {
		return nil, infraerrors.BadRequest("TOKENRHYTHM_API_KEY_NAME_INVALID", "TokenRhythm API key name is invalid")
	}
	resolved, err := s.ResolveTokenRhythmSession(ctx, sessionKey, proxyURL)
	if err != nil {
		return nil, err
	}
	session, csrf, err := parseTokenRhythmCookie(resolved.TokenRhythmCookie)
	if err != nil {
		return nil, fmt.Errorf("parse resolved TokenRhythm cookie: %w", err)
	}

	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return nil, fmt.Errorf("create TokenRhythm API key payload: %w", err)
	}
	requestCtx, cancel := context.WithTimeout(WithHTTPUpstreamRedirectsDisabled(ctx), tokenRhythmSessionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, tokenRhythmAPIKeyURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create TokenRhythm API key request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", resolved.TokenRhythmCookie)
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("Referer", "https://tokenrhythm.studio/account/keys")
	req.Header.Set("Origin", "https://tokenrhythm.studio")
	req.Header.Set("User-Agent", tokenRhythmSessionProbeUserAgent)

	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, 0, 1, nil)
	if err != nil {
		return nil, fmt.Errorf("TokenRhythm API key request failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("TokenRhythm API key request returned no response")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, tokenRhythmAPIKeyBodyLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read TokenRhythm API key response: %w", err)
	}
	if len(body) > tokenRhythmAPIKeyBodyLimit {
		return nil, infraerrors.New(http.StatusBadGateway, "TOKENRHYTHM_API_KEY_UPSTREAM_ERROR", "TokenRhythm API key response is too large")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, infraerrors.Unauthorized("TOKENRHYTHM_SESSION_INVALID", "TokenRhythm sess is invalid or expired")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, infraerrors.Newf(http.StatusBadGateway, "TOKENRHYTHM_API_KEY_UPSTREAM_ERROR", "TokenRhythm API key request failed with HTTP %d", resp.StatusCode)
	}

	var wire tokenRhythmAPIKeyWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "TOKENRHYTHM_API_KEY_RESPONSE_INVALID", "TokenRhythm returned an invalid API key response")
	}
	if code := tokenRhythmBalanceCode(wire.Code); code != "" && code != "0" {
		if strings.EqualFold(code, "UNAUTHORIZED") {
			return nil, infraerrors.Unauthorized("TOKENRHYTHM_SESSION_INVALID", "TokenRhythm sess is invalid or expired")
		}
		return nil, infraerrors.New(http.StatusBadGateway, "TOKENRHYTHM_API_KEY_UPSTREAM_ERROR", "TokenRhythm rejected the API key request")
	}
	apiKey := strings.TrimSpace(wire.Data.Key)
	if apiKey == "" {
		apiKey = strings.TrimSpace(wire.Key)
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(wire.Data.APIKey)
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(wire.APIKey)
	}
	if !strings.HasPrefix(apiKey, "sk_tr_") || len(apiKey) > 512 || containsControlCharacter(apiKey) {
		return nil, infraerrors.New(http.StatusBadGateway, "TOKENRHYTHM_API_KEY_MISSING", "TokenRhythm did not return a usable API key")
	}

	if rotatedSession, rotatedCSRF := tokenRhythmCookieUpdates(resp); rotatedSession != "" || rotatedCSRF != "" {
		if rotatedSession != "" {
			session = rotatedSession
		}
		if rotatedCSRF != "" {
			csrf = rotatedCSRF
		}
	}
	cookie := tokenRhythmSessionKey + "=" + session + "; " + tokenRhythmCSRFKey + "=" + csrf
	return &TokenRhythmAPIKeyResult{APIKey: apiKey, TokenRhythmCookie: cookie, Name: name}, nil
}

func tokenRhythmCookieUpdates(resp *http.Response) (string, string) {
	if resp == nil {
		return "", ""
	}
	session, csrf := "", ""
	for _, cookie := range resp.Cookies() {
		if cookie == nil || !isValidTokenRhythmCookieValue(cookie.Value) || containsControlCharacter(cookie.Value) {
			continue
		}
		switch cookie.Name {
		case tokenRhythmSessionKey:
			session = strings.TrimSpace(cookie.Value)
		case tokenRhythmCSRFKey:
			csrf = strings.TrimSpace(cookie.Value)
		}
	}
	return session, csrf
}

func containsControlCharacter(value string) bool {
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return true
		}
	}
	return false
}
