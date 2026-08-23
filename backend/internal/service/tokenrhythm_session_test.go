//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestResolveTokenRhythmSessionReturnsCookieAndReferralLink(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"Set-Cookie": []string{
				"tr_csrf=csrf-value; Path=/; Secure; SameSite=Lax",
				"tr_ref_device=device-value; Path=/; Secure",
			},
		},
		Body: io.NopCloser(bytes.NewBufferString(`{"code":0,"data":{"code":"invite/code","eligible":true,"publicEnabled":true,"registrationAllowed":true}}`)),
	}}
	svc := &AccountTestService{httpUpstream: upstream}

	result, err := svc.ResolveTokenRhythmSession(context.Background(), "sess_value", "socks5://proxy.example:1080")
	require.NoError(t, err)
	require.Equal(t, "tr_session=sess_value; tr_csrf=csrf-value", result.TokenRhythmCookie)
	require.Equal(t, "invite/code", result.ReferralCode)
	require.Equal(t, "https://tokenrhythm.studio/i/invite%2Fcode", result.ReferralLink)
	require.True(t, result.Eligible)
	require.True(t, result.PublicEnabled)
	require.True(t, result.RegistrationAllowed)
	require.Equal(t, tokenRhythmReferralURL, upstream.lastReq.URL.String())
	require.Equal(t, http.MethodGet, upstream.lastReq.Method)
	require.Equal(t, "tr_session=sess_value", upstream.lastReq.Header.Get("Cookie"))
	require.Empty(t, upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "https://tokenrhythm.studio/", upstream.lastReq.Header.Get("Referer"))
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
	require.Equal(t, "socks5://proxy.example:1080", upstream.lastProxyURL)
}

func TestResolveTokenRhythmSessionAcceptsRotatedSessionAndReferralAlias(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{"Set-Cookie": []string{
			"tr_session=sess_rotated; Path=/; Secure",
			"tr_csrf=csrf-rotated; Path=/; Secure",
		}},
		Body: io.NopCloser(strings.NewReader(`{"code":"0","data":{"referralCode":"abc_123","canInvite":true}}`)),
	}}
	svc := &AccountTestService{httpUpstream: upstream}

	result, err := svc.ResolveTokenRhythmSession(context.Background(), "sess_original", "")
	require.NoError(t, err)
	require.Equal(t, "tr_session=sess_rotated; tr_csrf=csrf-rotated", result.TokenRhythmCookie)
	require.Equal(t, "https://tokenrhythm.studio/i/abc_123", result.ReferralLink)
	require.True(t, result.Eligible)
}

func TestResolveTokenRhythmSessionRejectsUnsafeInput(t *testing.T) {
	svc := &AccountTestService{httpUpstream: &httpUpstreamRecorder{}}
	for _, session := range []string{"", "not-a-session", "sess_value; tr_csrf=injected", "sess_value\r\nCookie: stolen", "sess_" + strings.Repeat("x", tokenRhythmSessionInputLimit)} {
		_, err := svc.ResolveTokenRhythmSession(context.Background(), session, "")
		require.Error(t, err, session)
		require.Equal(t, http.StatusBadRequest, infraerrors.Code(err), session)
	}
}

func TestResolveTokenRhythmSessionRequiresCSRFCookie(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"code":0,"data":{"code":"abc"}}`)),
	}}
	svc := &AccountTestService{httpUpstream: upstream}

	_, err := svc.ResolveTokenRhythmSession(context.Background(), "sess_value", "")
	require.Error(t, err)
	require.Equal(t, http.StatusBadGateway, infraerrors.Code(err))
	require.Equal(t, "TOKENRHYTHM_SESSION_COOKIE_MISSING", infraerrors.Reason(err))
}

func TestResolveTokenRhythmSessionPreservesAuthenticationFailure(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"code":"UNAUTHORIZED","message":"expired secret"}`)),
	}}
	svc := &AccountTestService{httpUpstream: upstream}

	_, err := svc.ResolveTokenRhythmSession(context.Background(), "sess_value", "")
	require.Error(t, err)
	require.Equal(t, http.StatusUnauthorized, infraerrors.Code(err))
	require.Equal(t, "TOKENRHYTHM_SESSION_INVALID", infraerrors.Reason(err))
	require.NotContains(t, err.Error(), "expired secret")
}

func TestCreateTokenRhythmAPIKeyResolvesSessionAndCreatesKey(t *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusOK,
			Header: http.Header{"Set-Cookie": []string{
				"tr_session=sess_rotated; Path=/; Secure",
				"tr_csrf=csrf-value; Path=/; Secure",
			}},
			Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"code":"invite-code"}}`)),
		},
		{
			StatusCode: http.StatusOK,
			Header: http.Header{"Set-Cookie": []string{
				"tr_csrf=csrf-rotated; Path=/; Secure",
			}},
			Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"key":"sk_tr_created","name":"sub2api"}}`)),
		},
	}}
	svc := &AccountTestService{httpUpstream: upstream}

	result, err := svc.CreateTokenRhythmAPIKey(context.Background(), "sess_original", "sub2api", "socks5://proxy.example:1080")
	require.NoError(t, err)
	require.Equal(t, "sk_tr_created", result.APIKey)
	require.Equal(t, "tr_session=sess_rotated; tr_csrf=csrf-rotated", result.TokenRhythmCookie)
	require.Equal(t, "sub2api", result.Name)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, http.MethodGet, upstream.requests[0].Method)
	require.Equal(t, tokenRhythmReferralURL, upstream.requests[0].URL.String())
	require.Equal(t, http.MethodPost, upstream.requests[1].Method)
	require.Equal(t, tokenRhythmAPIKeyURL, upstream.requests[1].URL.String())
	require.Equal(t, "tr_session=sess_rotated; tr_csrf=csrf-value", upstream.requests[1].Header.Get("Cookie"))
	require.Equal(t, "csrf-value", upstream.requests[1].Header.Get("X-CSRF-Token"))
	require.Equal(t, "https://tokenrhythm.studio", upstream.requests[1].Header.Get("Origin"))
	require.JSONEq(t, `{"name":"sub2api"}`, string(upstream.bodies[0]))
	require.Equal(t, "socks5://proxy.example:1080", upstream.lastProxyURL)
}

func TestCreateTokenRhythmAPIKeyRejectsMissingProviderKey(t *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Set-Cookie": []string{"tr_csrf=csrf-value; Path=/; Secure"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":0,"data":{"code":"invite-code"}}`)),
		},
		{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"code":0,"data":{"name":"sub2api"}}`)),
		},
	}}
	svc := &AccountTestService{httpUpstream: upstream}

	_, err := svc.CreateTokenRhythmAPIKey(context.Background(), "sess_value", "sub2api", "")
	require.Error(t, err)
	require.Equal(t, http.StatusBadGateway, infraerrors.Code(err))
	require.Equal(t, "TOKENRHYTHM_API_KEY_MISSING", infraerrors.Reason(err))
}

func TestCreateTokenRhythmAPIKeyRejectsInvalidNameBeforeUpstream(t *testing.T) {
	upstream := &httpUpstreamRecorder{}
	svc := &AccountTestService{httpUpstream: upstream}

	_, err := svc.CreateTokenRhythmAPIKey(context.Background(), "sess_value", strings.Repeat("x", tokenRhythmAPIKeyNameLimit+1), "")
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Empty(t, upstream.requests)
}

func TestListTokenRhythmAPIKeysUsesCookieAndMasksSecrets(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":17,"name":"primary","key":"sk_tr_abcdefghijk9876","status":"active","createdAt":"2026-08-23T00:00:00Z"}]}`)),
	}}
	svc := &AccountTestService{httpUpstream: upstream}

	result, err := svc.ListTokenRhythmAPIKeys(context.Background(), "", "tr_session=sess_value; tr_csrf=csrf-value", "")
	require.NoError(t, err)
	require.Len(t, result.Keys, 1)
	require.Equal(t, "17", result.Keys[0].ID)
	require.Equal(t, "sk_tr_a...9876", result.Keys[0].MaskedKey)
	require.Equal(t, "tr_session=sess_value; tr_csrf=csrf-value", result.TokenRhythmCookie)
	require.Equal(t, http.MethodGet, upstream.lastReq.Method)
	require.Equal(t, tokenRhythmAPIKeyURL, upstream.lastReq.URL.String())
	require.Empty(t, upstream.lastReq.Header.Get("X-CSRF-Token"))
}

func TestParseTokenRhythmAPIKeyListSupportsProviderArrayAndCamelCase(t *testing.T) {
	keys, err := parseTokenRhythmAPIKeyList([]byte(`[{"id":9007199254740993,"name":"primary","maskedKey":"sk_tr_abcd****wxyz","keyPrefix":"sk_tr_abcd","status":"disabled","createdAt":"2026-08-23T00:00:00Z","lastUsedAt":"2026-08-23T01:00:00Z","deletedAt":"2026-08-23T02:00:00Z"}]`))
	require.NoError(t, err)
	require.Equal(t, []TokenRhythmAPIKeyListItem{{
		ID:         "9007199254740993",
		Name:       "primary",
		MaskedKey:  "sk_tr_abcd****wxyz",
		KeyPrefix:  "sk_tr_abcd",
		Status:     "disabled",
		CreatedAt:  "2026-08-23T00:00:00Z",
		LastUsedAt: "2026-08-23T01:00:00Z",
		DeletedAt:  "2026-08-23T02:00:00Z",
	}}, keys)
}

func TestParseTokenRhythmAPIKeyListMasksUnknownKeyFormats(t *testing.T) {
	keys, err := parseTokenRhythmAPIKeyList([]byte(`[{"id":"key-1","name":"primary","key":"provider-secret-value"},{"id":"key-2","name":"short","key":"secret"}]`))
	require.NoError(t, err)
	require.Equal(t, "pro...alue", keys[0].MaskedKey)
	require.Equal(t, "[redacted]", keys[1].MaskedKey)
}

func TestParseTokenRhythmAPIKeyListRejectsProviderBusinessError(t *testing.T) {
	_, err := parseTokenRhythmAPIKeyList([]byte(`{"code":4001,"message":"rejected","data":[]}`))
	require.Error(t, err)
	require.Equal(t, http.StatusBadGateway, infraerrors.Code(err))
	require.Equal(t, "TOKENRHYTHM_API_KEY_UPSTREAM_ERROR", infraerrors.Reason(err))

	_, err = parseTokenRhythmAPIKeyList([]byte(`{"code":"UNAUTHORIZED"}`))
	require.Error(t, err)
	require.Equal(t, http.StatusUnauthorized, infraerrors.Code(err))
	require.Equal(t, "TOKENRHYTHM_SESSION_INVALID", infraerrors.Reason(err))
}

func TestTokenRhythmAPIKeyMutationsUseProviderActionPaths(t *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0}`))},
		{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0}`))},
	}}
	svc := &AccountTestService{httpUpstream: upstream}

	_, err := svc.DisableTokenRhythmAPIKey(context.Background(), "", "tr_session=sess_value; tr_csrf=csrf-value", "key-17", "")
	require.NoError(t, err)
	_, err = svc.DeleteTokenRhythmAPIKey(context.Background(), "", "tr_session=sess_value; tr_csrf=csrf-value", "key-17", "")
	require.NoError(t, err)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, tokenRhythmAPIKeyURL+"/key-17/disable", upstream.requests[0].URL.String())
	require.Equal(t, tokenRhythmAPIKeyURL+"/key-17/delete", upstream.requests[1].URL.String())
	require.Equal(t, http.MethodPost, upstream.requests[0].Method)
	require.Equal(t, "csrf-value", upstream.requests[0].Header.Get("X-CSRF-Token"))
	require.Equal(t, "https://tokenrhythm.studio", upstream.requests[0].Header.Get("Origin"))
}

func TestTokenRhythmAPIKeyMutationRejectsUnsafeID(t *testing.T) {
	svc := &AccountTestService{httpUpstream: &httpUpstreamRecorder{}}
	_, err := svc.DeleteTokenRhythmAPIKey(context.Background(), "", "tr_session=sess_value; tr_csrf=csrf-value", "key-17/other", "")
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Equal(t, "TOKENRHYTHM_API_KEY_ID_INVALID", infraerrors.Reason(err))
}

type tokenRhythmAPIKeyAccountRepo struct {
	AccountRepository
	account            *Account
	updatedCredentials map[string]any
	updateErr          error
}

func (r *tokenRhythmAPIKeyAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.account == nil || r.account.ID != id {
		return nil, nil
	}
	return r.account, nil
}

func (r *tokenRhythmAPIKeyAccountRepo) UpdateCredentials(_ context.Context, id int64, credentials map[string]any) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	if r.account == nil || r.account.ID != id {
		return io.ErrUnexpectedEOF
	}
	r.updatedCredentials = shallowCopyMap(credentials)
	r.account.Credentials = shallowCopyMap(credentials)
	return nil
}

func TestCreateTokenRhythmAPIKeyForAccountReturnsCookieWhenPersistenceFails(t *testing.T) {
	repo := &tokenRhythmAPIKeyAccountRepo{
		account: &Account{ID: 24, Platform: PlatformTokenRhythm, Credentials: map[string]any{
			tokenRhythmSessionKey: "sess_stored",
			tokenRhythmCSRFKey:    "csrf-stored",
		}},
		updateErr: io.ErrUnexpectedEOF,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Set-Cookie": []string{"tr_csrf=csrf-rotated; Path=/; Secure"}},
		Body:       io.NopCloser(strings.NewReader(`{"code":0,"data":{"key":"sk_tr_created"}}`)),
	}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}

	result, err := svc.CreateTokenRhythmAPIKeyForAccount(context.Background(), 24, "sub2api", "")
	require.NoError(t, err)
	require.Equal(t, "sk_tr_created", result.APIKey)
	require.Equal(t, "tr_session=sess_stored; tr_csrf=csrf-rotated", result.TokenRhythmCookie)
	require.NotEmpty(t, result.CredentialPersistWarning)
}

func TestListTokenRhythmAPIKeysForAccountUsesStoredCookieAndDoesNotReturnIt(t *testing.T) {
	repo := &tokenRhythmAPIKeyAccountRepo{account: &Account{
		ID:          17,
		Platform:    PlatformTokenRhythm,
		Concurrency: 4,
		Proxy: &Proxy{
			Protocol: "socks5",
			Host:     "saved-proxy.example",
			Port:     1080,
		},
		Credentials: map[string]any{
			"api_key":             "sk_tr_existing",
			tokenRhythmSessionKey: "sess_stored",
			tokenRhythmCSRFKey:    "csrf-stored",
		},
	}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Set-Cookie": []string{"tr_csrf=csrf-rotated; Path=/; Secure"}},
		Body:       io.NopCloser(strings.NewReader(`[{"id":"key-17","name":"primary","maskedKey":"sk_tr_****"}]`)),
	}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}

	result, err := svc.ListTokenRhythmAPIKeysForAccount(context.Background(), 17, "socks5://proxy.example:1080")
	require.NoError(t, err)
	require.Len(t, result.Keys, 1)
	require.Empty(t, result.TokenRhythmCookie)
	require.Equal(t, "tr_session=sess_stored; tr_csrf=csrf-stored", upstream.lastReq.Header.Get("Cookie"))
	require.Equal(t, "socks5://proxy.example:1080", upstream.lastProxyURL)
	require.Equal(t, int64(17), upstream.lastAccountID)
	require.Equal(t, 4, upstream.lastConcurrency)
	require.Equal(t, "sess_stored", repo.updatedCredentials[tokenRhythmSessionKey])
	require.Equal(t, "csrf-rotated", repo.updatedCredentials[tokenRhythmCSRFKey])
	require.Equal(t, "sk_tr_existing", repo.updatedCredentials["api_key"])
}

func TestListTokenRhythmAPIKeysForAccountUsesSavedProxyWhenRequestOmitsProxy(t *testing.T) {
	repo := &tokenRhythmAPIKeyAccountRepo{account: &Account{
		ID:       18,
		Platform: PlatformTokenRhythm,
		Proxy: &Proxy{
			Protocol: "http",
			Host:     "saved-proxy.example",
			Port:     8080,
		},
		Credentials: map[string]any{
			tokenRhythmSessionKey: "sess_stored",
			tokenRhythmCSRFKey:    "csrf-stored",
		},
	}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
	}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}

	result, err := svc.ListTokenRhythmAPIKeysForAccount(context.Background(), 18, "")
	require.NoError(t, err)
	require.Empty(t, result.Keys)
	require.Equal(t, "http://saved-proxy.example:8080", upstream.lastProxyURL)
	require.Equal(t, int64(18), upstream.lastAccountID)
	require.Equal(t, 1, upstream.lastConcurrency)
}

func TestCreateTokenRhythmAPIKeyForAccountReturnsNewKeyWithoutStoredCookie(t *testing.T) {
	repo := &tokenRhythmAPIKeyAccountRepo{account: &Account{
		ID:       23,
		Platform: PlatformTokenRhythm,
		Credentials: map[string]any{
			tokenRhythmSessionKey: "sess_stored",
			tokenRhythmCSRFKey:    "csrf-stored",
		},
	}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"code":0,"data":{"key":"sk_tr_created"}}`)),
	}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}

	result, err := svc.CreateTokenRhythmAPIKeyForAccount(context.Background(), 23, "sub2api", "")
	require.NoError(t, err)
	require.Equal(t, "sk_tr_created", result.APIKey)
	require.Empty(t, result.TokenRhythmCookie)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, http.MethodPost, upstream.lastReq.Method)
}

func TestTokenRhythmAPIKeyAccountPathRejectsOtherPlatforms(t *testing.T) {
	repo := &tokenRhythmAPIKeyAccountRepo{account: &Account{ID: 31, Platform: PlatformOpenAI}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: &httpUpstreamRecorder{}}

	_, err := svc.ListTokenRhythmAPIKeysForAccount(context.Background(), 31, "")
	require.Error(t, err)
	require.Equal(t, http.StatusNotFound, infraerrors.Code(err))
	require.Equal(t, "TOKENRHYTHM_ACCOUNT_NOT_FOUND", infraerrors.Reason(err))
}
