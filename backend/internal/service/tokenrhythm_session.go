package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	tokenRhythmReferralURL           = "https://tokenrhythm.studio/api/referrals/me"
	tokenRhythmInviteBaseURL         = "https://tokenrhythm.studio/i/"
	tokenRhythmSessionInputLimit     = 4096
	tokenRhythmSessionBodyLimit      = 1 << 20
	tokenRhythmSessionTimeout        = 15 * time.Second
	tokenRhythmSessionProbeUserAgent = "Mozilla/5.0 (compatible; Sub2API TokenRhythm session resolver)"
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

func containsControlCharacter(value string) bool {
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return true
		}
	}
	return false
}
