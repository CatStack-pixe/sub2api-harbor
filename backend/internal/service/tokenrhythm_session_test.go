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
			Header: http.Header{"Set-Cookie": []string{"tr_csrf=csrf-value; Path=/; Secure"}},
			Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"code":"invite-code"}}`)),
		},
		{
			StatusCode: http.StatusOK,
			Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"name":"sub2api"}}`)),
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
