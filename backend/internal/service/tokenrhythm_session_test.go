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
