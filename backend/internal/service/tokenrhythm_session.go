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
	tokenRhythmReferralURL              = "https://tokenrhythm.studio/api/referrals/me"
	tokenRhythmInviteBaseURL            = "https://tokenrhythm.studio/i/"
	tokenRhythmSessionInputLimit        = 4096
	tokenRhythmSessionBodyLimit         = 1 << 20
	tokenRhythmSessionTimeout           = 15 * time.Second
	tokenRhythmSessionProbeUserAgent    = "Mozilla/5.0 (compatible; Sub2API TokenRhythm session resolver)"
	tokenRhythmAPIKeyURL                = "https://tokenrhythm.studio/api/api-keys"
	tokenRhythmAPIKeyNameLimit          = 20
	tokenRhythmAPIKeyBodyLimit          = 1 << 20
	tokenRhythmCredentialPersistWarning = "Provider operation succeeded, but the rotated TokenRhythm Cookie could not be saved. Save the returned Cookie with the account before retrying."
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
	APIKey                   string `json:"api_key"`
	TokenRhythmCookie        string `json:"tokenrhythm_cookie,omitempty"`
	Name                     string `json:"name"`
	CredentialPersisted      bool   `json:"credential_persisted,omitempty"`
	CredentialPersistWarning string `json:"credential_persist_warning,omitempty"`
}

// TokenRhythmAPIKeyListItem is the provider's non-secret API key metadata.
// The provider only returns the full secret during creation, so existing keys
// are intentionally represented by their masked value/prefix.
type TokenRhythmAPIKeyListItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	MaskedKey  string `json:"masked_key,omitempty"`
	KeyPrefix  string `json:"key_prefix,omitempty"`
	Status     string `json:"status,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	DeletedAt  string `json:"deleted_at,omitempty"`
}

type TokenRhythmAPIKeyListResult struct {
	Keys                     []TokenRhythmAPIKeyListItem `json:"keys"`
	TokenRhythmCookie        string                      `json:"tokenrhythm_cookie,omitempty"`
	CredentialPersistWarning string                      `json:"credential_persist_warning,omitempty"`
}

type TokenRhythmAPIKeyActionResult struct {
	ID                       string `json:"id"`
	TokenRhythmCookie        string `json:"tokenrhythm_cookie,omitempty"`
	CredentialPersisted      bool   `json:"credential_persisted,omitempty"`
	CredentialPersistWarning string `json:"credential_persist_warning,omitempty"`
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
	return s.CreateTokenRhythmAPIKeyWithCredential(ctx, sessionKey, "", name, proxyURL)
}

// CreateTokenRhythmAPIKeyWithCredential accepts either a fresh sess value or
// an already-resolved Cookie from an existing TokenRhythm account.
func (s *AccountTestService) CreateTokenRhythmAPIKeyWithCredential(ctx context.Context, sessionKey, cookieValue, name, proxyURL string) (*TokenRhythmAPIKeyResult, error) {
	return s.createTokenRhythmAPIKeyWithCredential(ctx, sessionKey, cookieValue, name, proxyURL, nil)
}

func (s *AccountTestService) createTokenRhythmAPIKeyWithCredential(ctx context.Context, sessionKey, cookieValue, name, proxyURL string, account *Account) (*TokenRhythmAPIKeyResult, error) {
	if s == nil || s.httpUpstream == nil {
		return nil, fmt.Errorf("upstream HTTP client is not configured")
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > tokenRhythmAPIKeyNameLimit || containsControlCharacter(name) {
		return nil, infraerrors.BadRequest("TOKENRHYTHM_API_KEY_NAME_INVALID", "TokenRhythm API key name is invalid")
	}
	resolvedCookie, err := s.resolveTokenRhythmAPIKeyCredential(ctx, sessionKey, cookieValue, proxyURL)
	if err != nil {
		return nil, err
	}
	session, csrf, err := parseTokenRhythmCookie(resolvedCookie)
	if err != nil {
		return nil, fmt.Errorf("parse resolved TokenRhythm cookie: %w", err)
	}

	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return nil, fmt.Errorf("create TokenRhythm API key payload: %w", err)
	}
	body, resp, err := s.doTokenRhythmAPIKeyRequest(ctx, http.MethodPost, tokenRhythmAPIKeyURL, resolvedCookie, proxyURL, payload, account)
	if err != nil {
		return nil, err
	}
	if err := tokenRhythmAPIKeyHTTPError(resp, "TokenRhythm API key create request"); err != nil {
		return nil, err
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

func (s *AccountTestService) resolveTokenRhythmAPIKeyCredential(ctx context.Context, sessionKey, cookieValue, proxyURL string) (string, error) {
	cookieValue = strings.TrimSpace(cookieValue)
	if cookieValue != "" {
		session, csrf, err := parseTokenRhythmCookie(cookieValue)
		if err != nil {
			return "", infraerrors.BadRequest("TOKENRHYTHM_COOKIE_INVALID", "TokenRhythm Cookie must include tr_session and tr_csrf")
		}
		return tokenRhythmCookieHeader(session, csrf), nil
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return "", infraerrors.BadRequest("TOKENRHYTHM_SESSION_REQUIRED", "TokenRhythm sess or Cookie is required")
	}
	resolved, err := s.ResolveTokenRhythmSession(ctx, sessionKey, proxyURL)
	if err != nil {
		return "", err
	}
	return resolved.TokenRhythmCookie, nil
}

func tokenRhythmCookieHeader(session, csrf string) string {
	return tokenRhythmSessionKey + "=" + session + "; " + tokenRhythmCSRFKey + "=" + csrf
}

func (s *AccountTestService) tokenRhythmCredentialForAccount(ctx context.Context, accountID int64) (*Account, string, error) {
	if accountID <= 0 {
		return nil, "", infraerrors.BadRequest("TOKENRHYTHM_ACCOUNT_INVALID", "TokenRhythm account ID is invalid")
	}
	if s == nil || s.accountRepo == nil {
		return nil, "", fmt.Errorf("account repository is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, "", err
	}
	if account == nil || !account.IsTokenRhythm() {
		return nil, "", infraerrors.NotFound("TOKENRHYTHM_ACCOUNT_NOT_FOUND", "TokenRhythm account not found")
	}
	session, csrf, err := TokenRhythmCookieCredentials(account.Credentials)
	if err != nil {
		return nil, "", infraerrors.BadRequest("TOKENRHYTHM_COOKIE_INVALID", "The account does not have a valid TokenRhythm Cookie")
	}
	return account, tokenRhythmCookieHeader(session, csrf), nil
}

func (s *AccountTestService) ListTokenRhythmAPIKeys(ctx context.Context, sessionKey, cookieValue, proxyURL string) (*TokenRhythmAPIKeyListResult, error) {
	return s.listTokenRhythmAPIKeysWithAccount(ctx, sessionKey, cookieValue, proxyURL, nil)
}

func (s *AccountTestService) listTokenRhythmAPIKeysWithAccount(ctx context.Context, sessionKey, cookieValue, proxyURL string, account *Account) (*TokenRhythmAPIKeyListResult, error) {
	cookie, err := s.resolveTokenRhythmAPIKeyCredential(ctx, sessionKey, cookieValue, proxyURL)
	if err != nil {
		return nil, err
	}
	body, resp, err := s.doTokenRhythmAPIKeyRequest(ctx, http.MethodGet, tokenRhythmAPIKeyURL, cookie, proxyURL, nil, account)
	if err != nil {
		return nil, err
	}
	if err := tokenRhythmAPIKeyHTTPError(resp, "TokenRhythm API key list request"); err != nil {
		return nil, err
	}
	keys, err := parseTokenRhythmAPIKeyList(body)
	if err != nil {
		return nil, err
	}
	if rotatedSession, rotatedCSRF := tokenRhythmCookieUpdates(resp); rotatedSession != "" || rotatedCSRF != "" {
		session, csrf, parseErr := parseTokenRhythmCookie(cookie)
		if parseErr == nil {
			if rotatedSession != "" {
				session = rotatedSession
			}
			if rotatedCSRF != "" {
				csrf = rotatedCSRF
			}
			cookie = tokenRhythmCookieHeader(session, csrf)
		}
	}
	return &TokenRhythmAPIKeyListResult{Keys: keys, TokenRhythmCookie: cookie}, nil
}

func (s *AccountTestService) ListTokenRhythmAPIKeysForAccount(ctx context.Context, accountID int64, proxyURL string) (*TokenRhythmAPIKeyListResult, error) {
	account, cookie, err := s.tokenRhythmCredentialForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(proxyURL) == "" && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	result, err := s.listTokenRhythmAPIKeysWithAccount(ctx, "", cookie, proxyURL, account)
	if err != nil {
		return nil, err
	}
	if err := s.persistTokenRhythmCookieForAccount(ctx, accountID, result.TokenRhythmCookie); err != nil {
		result.CredentialPersistWarning = tokenRhythmCredentialPersistWarning
		return result, nil
	}
	result.TokenRhythmCookie = ""
	return result, nil
}

func (s *AccountTestService) CreateTokenRhythmAPIKeyForAccount(ctx context.Context, accountID int64, name, proxyURL string) (*TokenRhythmAPIKeyResult, error) {
	account, cookie, err := s.tokenRhythmCredentialForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(proxyURL) == "" && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	result, err := s.createTokenRhythmAPIKeyWithCredential(ctx, "", cookie, name, proxyURL, account)
	if err != nil {
		return nil, err
	}
	if err := s.persistTokenRhythmCookieForAccount(ctx, accountID, result.TokenRhythmCookie); err != nil {
		result.CredentialPersistWarning = tokenRhythmCredentialPersistWarning
		return result, nil
	}
	result.CredentialPersisted = true
	result.TokenRhythmCookie = ""
	return result, nil
}

func (s *AccountTestService) tokenRhythmAPIKeyAction(ctx context.Context, sessionKey, cookieValue, id, action, proxyURL string) (*TokenRhythmAPIKeyActionResult, error) {
	return s.tokenRhythmAPIKeyActionWithAccount(ctx, sessionKey, cookieValue, id, action, proxyURL, nil)
}

func (s *AccountTestService) tokenRhythmAPIKeyActionWithAccount(ctx context.Context, sessionKey, cookieValue, id, action, proxyURL string, account *Account) (*TokenRhythmAPIKeyActionResult, error) {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 256 || containsControlCharacter(id) || strings.ContainsAny(id, "/\\?#") {
		return nil, infraerrors.BadRequest("TOKENRHYTHM_API_KEY_ID_INVALID", "TokenRhythm API key ID is invalid")
	}
	if action != "disable" && action != "delete" {
		return nil, infraerrors.BadRequest("TOKENRHYTHM_API_KEY_ACTION_INVALID", "TokenRhythm API key action is invalid")
	}
	cookie, err := s.resolveTokenRhythmAPIKeyCredential(ctx, sessionKey, cookieValue, proxyURL)
	if err != nil {
		return nil, err
	}
	target := tokenRhythmAPIKeyURL + "/" + url.PathEscape(id) + "/" + action
	body, resp, err := s.doTokenRhythmAPIKeyRequest(ctx, http.MethodPost, target, cookie, proxyURL, nil, account)
	if err != nil {
		return nil, err
	}
	if err := tokenRhythmAPIKeyHTTPError(resp, "TokenRhythm API key "+action+" request"); err != nil {
		return nil, err
	}
	if err := parseTokenRhythmAPIKeyActionResponse(body); err != nil {
		return nil, err
	}
	if rotatedSession, rotatedCSRF := tokenRhythmCookieUpdates(resp); rotatedSession != "" || rotatedCSRF != "" {
		session, csrf, parseErr := parseTokenRhythmCookie(cookie)
		if parseErr == nil {
			if rotatedSession != "" {
				session = rotatedSession
			}
			if rotatedCSRF != "" {
				csrf = rotatedCSRF
			}
			cookie = tokenRhythmCookieHeader(session, csrf)
		}
	}
	return &TokenRhythmAPIKeyActionResult{ID: id, TokenRhythmCookie: cookie}, nil
}

func (s *AccountTestService) DisableTokenRhythmAPIKey(ctx context.Context, sessionKey, cookieValue, id, proxyURL string) (*TokenRhythmAPIKeyActionResult, error) {
	return s.tokenRhythmAPIKeyAction(ctx, sessionKey, cookieValue, id, "disable", proxyURL)
}

func (s *AccountTestService) DeleteTokenRhythmAPIKey(ctx context.Context, sessionKey, cookieValue, id, proxyURL string) (*TokenRhythmAPIKeyActionResult, error) {
	return s.tokenRhythmAPIKeyAction(ctx, sessionKey, cookieValue, id, "delete", proxyURL)
}

func (s *AccountTestService) DisableTokenRhythmAPIKeyForAccount(ctx context.Context, accountID int64, id, proxyURL string) (*TokenRhythmAPIKeyActionResult, error) {
	account, cookie, err := s.tokenRhythmCredentialForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(proxyURL) == "" && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	result, err := s.tokenRhythmAPIKeyActionWithAccount(ctx, "", cookie, id, "disable", proxyURL, account)
	if err != nil {
		return nil, err
	}
	if err := s.persistTokenRhythmCookieForAccount(ctx, accountID, result.TokenRhythmCookie); err != nil {
		result.CredentialPersistWarning = tokenRhythmCredentialPersistWarning
		return result, nil
	}
	result.CredentialPersisted = true
	result.TokenRhythmCookie = ""
	return result, nil
}

func (s *AccountTestService) DeleteTokenRhythmAPIKeyForAccount(ctx context.Context, accountID int64, id, proxyURL string) (*TokenRhythmAPIKeyActionResult, error) {
	account, cookie, err := s.tokenRhythmCredentialForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(proxyURL) == "" && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	result, err := s.tokenRhythmAPIKeyActionWithAccount(ctx, "", cookie, id, "delete", proxyURL, account)
	if err != nil {
		return nil, err
	}
	if err := s.persistTokenRhythmCookieForAccount(ctx, accountID, result.TokenRhythmCookie); err != nil {
		result.CredentialPersistWarning = tokenRhythmCredentialPersistWarning
		return result, nil
	}
	result.CredentialPersisted = true
	result.TokenRhythmCookie = ""
	return result, nil
}

func (s *AccountTestService) persistTokenRhythmCookieForAccount(ctx context.Context, accountID int64, cookie string) error {
	if strings.TrimSpace(cookie) == "" {
		return nil
	}
	if s == nil || s.accountRepo == nil {
		return fmt.Errorf("account repository is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account == nil || !account.IsTokenRhythm() {
		return infraerrors.NotFound("TOKENRHYTHM_ACCOUNT_NOT_FOUND", "TokenRhythm account not found")
	}
	session, csrf, err := parseTokenRhythmCookie(cookie)
	if err != nil {
		return infraerrors.BadRequest("TOKENRHYTHM_COOKIE_INVALID", "TokenRhythm returned an invalid Cookie")
	}
	oldSession, oldCSRF, oldErr := TokenRhythmCookieCredentials(account.Credentials)
	if oldErr == nil && oldSession == session && oldCSRF == csrf {
		return nil
	}
	credentials := make(map[string]any, len(account.Credentials)+2)
	for key, value := range account.Credentials {
		credentials[key] = value
	}
	credentials[tokenRhythmSessionKey] = session
	credentials[tokenRhythmCSRFKey] = csrf
	delete(credentials, tokenRhythmCookieInputKey)
	return persistAccountCredentials(ctx, s.accountRepo, account, credentials)
}

func (s *AccountTestService) doTokenRhythmAPIKeyRequest(ctx context.Context, method, target, cookie, proxyURL string, payload []byte, account *Account) ([]byte, *http.Response, error) {
	if s == nil || s.httpUpstream == nil {
		return nil, nil, fmt.Errorf("upstream HTTP client is not configured")
	}
	requestCtx, cancel := context.WithTimeout(WithHTTPUpstreamRedirectsDisabled(ctx), tokenRhythmSessionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, target, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, fmt.Errorf("create TokenRhythm API key request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Referer", "https://tokenrhythm.studio/account/keys")
	req.Header.Set("User-Agent", tokenRhythmSessionProbeUserAgent)
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", csrfFromTokenRhythmCookie(cookie))
		req.Header.Set("Origin", "https://tokenrhythm.studio")
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	accountID := int64(0)
	accountConcurrency := 1
	if account != nil {
		accountID = account.ID
		if account.Concurrency > 0 {
			accountConcurrency = account.Concurrency
		}
		if s.tlsFPProfileService != nil {
			resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, accountID, accountConcurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
			if err != nil {
				return nil, nil, fmt.Errorf("TokenRhythm API key request failed: %w", err)
			}
			return s.readTokenRhythmAPIKeyResponse(resp)
		}
	}
	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("TokenRhythm API key request failed: %w", err)
	}
	return s.readTokenRhythmAPIKeyResponse(resp)
}

func (s *AccountTestService) readTokenRhythmAPIKeyResponse(resp *http.Response) ([]byte, *http.Response, error) {
	if resp == nil {
		return nil, nil, fmt.Errorf("TokenRhythm API key request returned no response")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, tokenRhythmAPIKeyBodyLimit+1))
	if err != nil {
		return nil, resp, fmt.Errorf("read TokenRhythm API key response: %w", err)
	}
	if len(body) > tokenRhythmAPIKeyBodyLimit {
		return nil, resp, infraerrors.New(http.StatusBadGateway, "TOKENRHYTHM_API_KEY_UPSTREAM_ERROR", "TokenRhythm API key response is too large")
	}
	return body, resp, nil
}

func csrfFromTokenRhythmCookie(cookie string) string {
	_, csrf, err := parseTokenRhythmCookie(cookie)
	if err != nil {
		return ""
	}
	return csrf
}

func tokenRhythmAPIKeyHTTPError(resp *http.Response, operation string) error {
	if resp == nil {
		return fmt.Errorf("%s returned no response", operation)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return infraerrors.Unauthorized("TOKENRHYTHM_SESSION_INVALID", "TokenRhythm Cookie is invalid or expired")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return infraerrors.Newf(http.StatusBadGateway, "TOKENRHYTHM_API_KEY_UPSTREAM_ERROR", "%s failed with HTTP %d", operation, resp.StatusCode)
	}
	return nil
}

func parseTokenRhythmAPIKeyList(body []byte) ([]TokenRhythmAPIKeyListItem, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "TOKENRHYTHM_API_KEY_RESPONSE_INVALID", "TokenRhythm returned an invalid API key list response")
	}
	if object, ok := root.(map[string]any); ok {
		if code := tokenRhythmResponseCode(object["code"]); code == "UNAUTHORIZED" {
			return nil, infraerrors.Unauthorized("TOKENRHYTHM_SESSION_INVALID", "TokenRhythm Cookie is invalid or expired")
		}
		if code := tokenRhythmResponseCode(object["code"]); code != "" && code != "0" {
			return nil, infraerrors.New(http.StatusBadGateway, "TOKENRHYTHM_API_KEY_UPSTREAM_ERROR", "TokenRhythm rejected the API key list request")
		}
		for _, key := range []string{"data", "keys", "api_keys", "items"} {
			if value, exists := object[key]; exists {
				if nested, ok := value.(map[string]any); ok {
					for _, nestedKey := range []string{"keys", "api_keys", "items"} {
						if items, ok := nested[nestedKey].([]any); ok {
							return tokenRhythmAPIKeyListItems(items), nil
						}
					}
				}
				if items, ok := value.([]any); ok {
					return tokenRhythmAPIKeyListItems(items), nil
				}
			}
		}
	}
	if items, ok := root.([]any); ok {
		return tokenRhythmAPIKeyListItems(items), nil
	}
	return nil, infraerrors.New(http.StatusBadGateway, "TOKENRHYTHM_API_KEY_RESPONSE_INVALID", "TokenRhythm returned an invalid API key list response")
}

func tokenRhythmAPIKeyListItems(raw []any) []TokenRhythmAPIKeyListItem {
	items := make([]TokenRhythmAPIKeyListItem, 0, len(raw))
	for _, value := range raw {
		object, ok := value.(map[string]any)
		if !ok {
			continue
		}
		item := TokenRhythmAPIKeyListItem{
			ID:         tokenRhythmMapString(object, "id"),
			Name:       tokenRhythmMapString(object, "name"),
			MaskedKey:  tokenRhythmMapString(object, "maskedKey", "masked_key", "key"),
			KeyPrefix:  tokenRhythmMapString(object, "keyPrefix", "key_prefix"),
			Status:     tokenRhythmMapString(object, "status"),
			CreatedAt:  tokenRhythmMapString(object, "createdAt", "created_at"),
			LastUsedAt: tokenRhythmMapString(object, "lastUsedAt", "last_used_at"),
			DeletedAt:  tokenRhythmMapString(object, "deletedAt", "deleted_at"),
		}
		if item.ID == "" {
			continue
		}
		if item.MaskedKey != "" && !strings.Contains(item.MaskedKey, "*") {
			item.MaskedKey = maskTokenRhythmAPIKey(item.MaskedKey)
		}
		items = append(items, item)
	}
	return items
}

func tokenRhythmMapString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := object[key]
		if !ok || value == nil {
			continue
		}
		if stringValue, ok := value.(string); ok {
			return strings.TrimSpace(stringValue)
		}
		if numberValue, ok := value.(json.Number); ok {
			return numberValue.String()
		}
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return ""
}

func maskTokenRhythmAPIKey(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 10 {
		return "[redacted]"
	}
	if strings.Contains(value, "*") {
		return value
	}
	if strings.HasPrefix(value, "sk_tr_") {
		return value[:7] + "..." + value[len(value)-4:]
	}
	return value[:3] + "..." + value[len(value)-4:]
}

func parseTokenRhythmAPIKeyActionResponse(body []byte) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil {
		return infraerrors.New(http.StatusBadGateway, "TOKENRHYTHM_API_KEY_RESPONSE_INVALID", "TokenRhythm returned an invalid API key response")
	}
	if code := tokenRhythmResponseCode(object["code"]); code != "" && code != "0" {
		if code == "UNAUTHORIZED" {
			return infraerrors.Unauthorized("TOKENRHYTHM_SESSION_INVALID", "TokenRhythm Cookie is invalid or expired")
		}
		return infraerrors.New(http.StatusBadGateway, "TOKENRHYTHM_API_KEY_UPSTREAM_ERROR", "TokenRhythm rejected the API key action")
	}
	return nil
}

func tokenRhythmResponseCode(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.ToUpper(strings.TrimSpace(typed))
	case json.Number:
		return typed.String()
	case float64:
		return fmt.Sprintf("%g", typed)
	default:
		return strings.ToUpper(strings.TrimSpace(fmt.Sprint(typed)))
	}
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
