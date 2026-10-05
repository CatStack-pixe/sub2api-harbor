//go:build unit

package service

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type traceUpstreamStub struct {
	requests []*http.Request
}

func (s *traceUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.requests = append(s.requests, req)
	if req.URL.Path == "/v1/models" {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"astra"},{"id":"astra"}]}`)),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"model":"astra","usage":{"total_tokens":2}}`)),
	}, nil
}

func (s *traceUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestRunUpstreamTraceUsesSingleModelFieldAndSafeRequestContext(t *testing.T) {
	upstream := &traceUpstreamStub{}
	service := &AccountTestService{
		httpUpstream: upstream,
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true},
			},
		},
	}

	result, err := service.RunUpstreamTrace(t.Context(), 7243, UpstreamTraceRequest{
		UpstreamBaseURL: "http://example.com/v1",
		APIKey:          "secret-token",
		RequestModel:    "astra",
		Prompt:          "trace",
	})
	require.NoError(t, err)
	require.Equal(t, "/v1/chat/completions", result.RequestPath)
	require.Equal(t, "astra", result.ResponseModel)
	require.Contains(t, result.Models, "astra")
	require.Len(t, upstream.requests, 2)
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.requests[0].Context()))
	require.True(t, HTTPUpstreamPublicHostsOnly(upstream.requests[0].Context()))
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.requests[1].Context()))

	body, err := io.ReadAll(upstream.requests[1].Body)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(body), `"model"`))
	require.Equal(t, "Bearer secret-token", upstream.requests[1].Header.Get("Authorization"))
}

func TestRunUpstreamTraceRejectsURLCredentialsAndQuery(t *testing.T) {
	service := &AccountTestService{
		httpUpstream: &traceUpstreamStub{},
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true},
			},
		},
	}
	_, err := service.RunUpstreamTrace(t.Context(), 1, UpstreamTraceRequest{
		UpstreamBaseURL: "https://user:pass@example.com/v1?token=leak",
		APIKey:          "secret",
		RequestModel:    "astra",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "credentials")
}
