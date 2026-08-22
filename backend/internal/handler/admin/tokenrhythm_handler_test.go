//go:build unit

package admin

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type tokenRhythmSessionHandlerUpstream struct {
	request *http.Request
}

func (u *tokenRhythmSessionHandlerUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.request = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{"Set-Cookie": []string{
			"tr_csrf=csrf-value; Path=/; Secure",
		}},
		Body: io.NopCloser(bytes.NewBufferString(`{"code":0,"data":{"code":"invite-code","eligible":true}}`)),
	}, nil
}

func (u *tokenRhythmSessionHandlerUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestResolveTokenRhythmSessionHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &tokenRhythmSessionHandlerUpstream{}
	testService := service.NewAccountTestService(nil, nil, nil, nil, nil, upstream, nil, nil)
	handler := NewAccountHandler(nil, nil, nil, nil, nil, nil, nil, nil, testService, nil, nil, nil, nil, nil)
	router := gin.New()
	router.POST("/session/resolve", handler.ResolveTokenRhythmSession)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/session/resolve", bytes.NewBufferString(`{"sess":"sess_value"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.JSONEq(t, `{
		"code":0,
		"message":"success",
		"data":{
			"tokenrhythm_cookie":"tr_session=sess_value; tr_csrf=csrf-value",
			"referral_code":"invite-code",
			"referral_link":"https://tokenrhythm.studio/i/invite-code",
			"eligible":true,
			"public_enabled":false,
			"registration_allowed":false
		}
	}`, response.Body.String())
	require.NotNil(t, upstream.request)
}

func TestResolveTokenRhythmSessionHandlerRejectsMissingSess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAccountHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.POST("/session/resolve", handler.ResolveTokenRhythmSession)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/session/resolve", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}
