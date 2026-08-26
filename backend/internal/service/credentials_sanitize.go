package service

import (
	"fmt"
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
