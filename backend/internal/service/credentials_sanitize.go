package service

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	tokenRhythmCookieInputKey = "tokenrhythm_cookie"
	tokenRhythmSessionKey     = "tr_session"
	tokenRhythmCSRFKey        = "tr_csrf"
)

// SanitizeStoredCredentials strips secrets that must never be persisted on the
// account credentials map after conversion to OAuth tokens (Grok Web SSO / password).
// Call from admin create/update/import/apply-oauth paths.
//
// Cookie is always stripped: bulk paths may pass an empty platform label, and
// session-jar residue must never sit next to OAuth tokens on any platform.
// The platform argument is retained for call-site clarity / future scrubbing.
func SanitizeStoredCredentials(platform string, creds map[string]any) map[string]any {
	if creds == nil {
		return nil
	}
	if platform == PlatformTierflow {
		if session, userID, err := TierflowCookieCredentials(creds); err == nil {
			creds["tierflow_cookie"] = session
			creds["tierflow_user_id"] = userID
		}
	} else {
		delete(creds, "tierflow_cookie")
		delete(creds, "tierflow_user_id")
	}
	if platform == PlatformTokenRhythm {
		// The UI submits a complete Cookie header. Persist only the two values
		// required by the balance endpoint, never the raw header or unrelated
		// browser cookies.
		if session, csrf, err := TokenRhythmCookieCredentials(creds); err == nil {
			creds[tokenRhythmSessionKey] = session
			creds[tokenRhythmCSRFKey] = csrf
			creds["base_url"] = TokenRhythmDefaultBaseURL
			delete(creds, tokenRhythmCookieInputKey)
		}
	} else {
		// This input-only field must never be retained by generic or bulk update
		// paths, even when their platform context is unavailable.
		delete(creds, tokenRhythmCookieInputKey)
	}
	for _, key := range []string{
		"password", "sso_token", "sso", "sso-rw", "clearTextPassword", "cookie",
	} {
		delete(creds, key)
	}
	return creds
}

// TierflowCookieCredentials accepts a raw session value or a browser Cookie
// header, keeping only the session cookie needed by the official console API.
func TierflowCookieCredentials(creds map[string]any) (string, string, error) {
	raw, ok := creds["tierflow_cookie"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return "", "", fmt.Errorf("Tierflow session Cookie is required for balance queries")
	}
	if strings.ContainsAny(raw, "\r\n") {
		return "", "", fmt.Errorf("Tierflow Cookie contains an invalid line break")
	}
	session := strings.TrimSpace(raw)
	if strings.HasPrefix(session, "session=") || strings.Contains(session, ";") {
		session = ""
		foundSession := false
		for _, segment := range strings.Split(raw, ";") {
			name, value, found := strings.Cut(strings.TrimSpace(segment), "=")
			if !found || name != "session" {
				continue
			}
			if foundSession {
				return "", "", fmt.Errorf("Tierflow Cookie contains duplicate session values")
			}
			foundSession = true
			session = value
		}
	}
	if session == "" || len(session) > 16*1024 {
		return "", "", fmt.Errorf("Tierflow session Cookie is invalid")
	}
	for _, char := range session {
		if char < 0x21 || char > 0x7e || strings.ContainsRune("\";,\\", char) {
			return "", "", fmt.Errorf("Tierflow session Cookie is invalid")
		}
	}
	userID, ok := creds["tierflow_user_id"].(string)
	if !ok {
		return "", "", fmt.Errorf("Tierflow user ID must be a positive numeric string")
	}
	userID = strings.TrimSpace(userID)
	for _, char := range userID {
		if char < '0' || char > '9' {
			return "", "", fmt.Errorf("Tierflow user ID must be a positive numeric string")
		}
	}
	numericID, err := strconv.ParseUint(userID, 10, 64)
	if err != nil || numericID == 0 {
		return "", "", fmt.Errorf("Tierflow user ID must be a positive numeric string")
	}
	return session, strconv.FormatUint(numericID, 10), nil
}

// TokenRhythmCookieCredentials extracts the only two cookies accepted by the
// TokenRhythm usage endpoint. It accepts either the raw admin form value or
// already-normalized stored credentials so validation works on create and edit.
func TokenRhythmCookieCredentials(creds map[string]any) (string, string, error) {
	if creds == nil {
		return "", "", fmt.Errorf("TokenRhythm Cookie is required")
	}
	if raw, exists := creds[tokenRhythmCookieInputKey]; exists {
		cookie, ok := raw.(string)
		if !ok {
			return "", "", fmt.Errorf("TokenRhythm Cookie must be a string")
		}
		return parseTokenRhythmCookie(cookie)
	}
	session, _ := creds[tokenRhythmSessionKey].(string)
	csrf, _ := creds[tokenRhythmCSRFKey].(string)
	if !isValidTokenRhythmCookieValue(session) || !isValidTokenRhythmCookieValue(csrf) {
		return "", "", fmt.Errorf("TokenRhythm Cookie must include tr_session and tr_csrf")
	}
	return strings.TrimSpace(session), strings.TrimSpace(csrf), nil
}

func parseTokenRhythmCookie(raw string) (string, string, error) {
	if strings.ContainsAny(raw, "\r\n") {
		return "", "", fmt.Errorf("TokenRhythm Cookie contains an invalid line break")
	}
	values := make(map[string]string, 2)
	for _, segment := range strings.Split(raw, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(segment), "=")
		if !ok || name == "" {
			continue
		}
		if name != tokenRhythmSessionKey && name != tokenRhythmCSRFKey {
			continue
		}
		if _, duplicate := values[name]; duplicate {
			return "", "", fmt.Errorf("TokenRhythm Cookie contains duplicate %s", name)
		}
		if !isValidTokenRhythmCookieValue(value) {
			return "", "", fmt.Errorf("TokenRhythm Cookie contains an invalid %s", name)
		}
		values[name] = strings.TrimSpace(value)
	}
	session, sessionOK := values[tokenRhythmSessionKey]
	csrf, csrfOK := values[tokenRhythmCSRFKey]
	if !sessionOK || !csrfOK {
		return "", "", fmt.Errorf("TokenRhythm Cookie must include tr_session and tr_csrf")
	}
	return session, csrf, nil
}

func isValidTokenRhythmCookieValue(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.ContainsAny(value, ";\r\n")
}

// sanitizeProviderManagedCredentials prevents generic account CRUD from
// accepting provider-derived Grok OAuth state. Trusted OAuth exchange and
// refresh flows opt in explicitly after deriving these values from tokens.
func sanitizeProviderManagedCredentials(platform string, creds map[string]any, trusted bool) map[string]any {
	if creds == nil || trusted || platform != PlatformGrok {
		return creds
	}
	delete(creds, "subscription_tier")
	delete(creds, "entitlement_status")
	return creds
}
