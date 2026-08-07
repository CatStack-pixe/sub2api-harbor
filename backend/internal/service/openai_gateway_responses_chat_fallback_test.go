//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardResponses_ForceChatCompletionsRoutesNonStreamingToChatCompletions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_resp_chat_json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_json","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5,"prompt_tokens_details":{"cached_tokens":1}}}`,
		)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "http://upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
	require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "messages.0.content").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
	require.Equal(t, "response", gjson.Get(rec.Body.String(), "object").String())
	require.Equal(t, "ok", gjson.Get(rec.Body.String(), "output.0.content.0.text").String())
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, 1, result.Usage.CacheReadInputTokens)
	require.False(t, result.Stream)
}

func TestForwardResponses_ForceChatCompletionsRoutesStreamingToChatCompletions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"he"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"llo"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_resp_chat_stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "http://upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream_options.include_usage").Bool())
	require.Contains(t, rec.Body.String(), "event: response.output_text.delta")
	require.Contains(t, rec.Body.String(), `"delta":"he"`)
	require.Contains(t, rec.Body.String(), "event: response.completed")
	require.Contains(t, rec.Body.String(), `"input_tokens":4`)
	require.Contains(t, rec.Body.String(), "data: [DONE]")
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.True(t, result.Stream)
	require.NotNil(t, result.FirstTokenMs)
}

func TestForwardResponses_DeepSeekReasoningOnlyStreamProducesVisibleText(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"deepseek-reasoner","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_reasoning","object":"chat.completion.chunk","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"role":"assistant","content":null,"reasoning_content":""},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_reasoning","object":"chat.completion.chunk","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"reasoning_content":"visible fallback"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_reasoning","object":"chat.completion.chunk","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"content":""},"finish_reason":"length"}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_deepseek_reasoning_responses_stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Contains(t, rec.Body.String(), "event: response.output_text.delta")
	require.Contains(t, rec.Body.String(), `"delta":"visible fallback"`)
	require.Contains(t, rec.Body.String(), `"status":"incomplete"`)
	require.Contains(t, rec.Body.String(), "data: [DONE]")
}

func TestForwardResponses_AutoSupportedAccountStillUsesResponsesEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_resp_native"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_native","object":"response","model":"gpt-5.4","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}],"status":"completed"}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`,
		)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{
		openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeAuto),
		openai_compat.ExtraKeyResponsesSupported: true,
	}

	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "http://upstream.example/v1/responses", upstream.lastReq.URL.String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "messages").Exists())
	require.Equal(t, "ok", gjson.Get(rec.Body.String(), "output.0.content.0.text").String())
}

func forceChatResponsesFallbackAccount() *Account {
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{
		openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions),
	}
	return account
}

func waitForFallbackRecorderBody(t *testing.T, recorder *openAIResponseFlushRecorder, want string) string {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		body, _ := recorder.snapshot()
		if strings.Contains(body, want) {
			return body
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for %q in response body; got %q", want, body)
		case <-ticker.C:
		}
	}
}

func waitForFallbackGate(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for upstream stream gate")
	}
}

func TestForwardResponses_NvidiaKeepaliveBeforeFirstTokenAndBetweenChunks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := newOpenAIResponseFlushRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	firstGate := make(chan struct{})
	secondGate := make(chan struct{})
	waiting := []chan struct{}{make(chan struct{}), make(chan struct{})}
	streamBody := &stagedOpenAISSEReadCloser{
		segments: [][]byte{
			[]byte(`data: {"id":"chatcmpl_nim","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"one"},"finish_reason":null}]}` + "\n\n"),
			[]byte(`data: {"id":"chatcmpl_nim","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"two"},"finish_reason":null}]}` + "\n\n"),
			[]byte(`data: {"id":"chatcmpl_nim","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}` + "\n\n" + "data: [DONE]\n\n"),
		},
		gates:   []<-chan struct{}{firstGate, secondGate, nil},
		waiting: waiting,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_nim_keepalive"}},
		Body:       streamBody,
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := forceChatResponsesFallbackAccount()
	account.Platform = PlatformNvidia
	stop := StartOpenAIResponsesSSEKeepalive(c, 15*time.Millisecond)
	defer stop()

	resultCh := make(chan *OpenAIForwardResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"nvidia/test","input":"hello","stream":true}`))
		resultCh <- result
		errCh <- err
	}()

	waitForFallbackGate(t, waiting[0])
	body := waitForFallbackRecorderBody(t, recorder, ": keepalive\n\n")
	require.NotContains(t, body, "event:")
	close(firstGate)
	waitForFallbackRecorderBody(t, recorder, `"delta":"one"`)
	waitForFallbackGate(t, waiting[1])
	body = waitForFallbackRecorderBody(t, recorder, `"delta":"one"`)
	time.Sleep(25 * time.Millisecond)
	body, _ = recorder.snapshot()
	require.GreaterOrEqual(t, strings.Count(body, ": keepalive\n\n"), 2, "keepalive must continue during chunk silence")
	close(secondGate)

	result := <-resultCh
	require.NoError(t, <-errCh)
	require.NotNil(t, result)
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.NotNil(t, result.FirstTokenMs, "keepalive comments must not become the first token")
	body, _ = recorder.snapshot()
	require.Contains(t, body, "response.completed")
	require.Contains(t, body, "data: [DONE]\n\n")
	assertedEvents := stripKeepaliveComments(body)
	require.NotContains(t, assertedEvents, ": keepalive")
}

func TestForwardResponses_NvidiaKeepaliveDrainsUsageAfterClientDisconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := newOpenAIResponseFlushRecorder()
	c, cancel := context.WithCancel(httptest.NewRequest(http.MethodPost, "/v1/responses", nil).Context())
	defer cancel()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(c)
	gate := make(chan struct{})
	streamBody := &stagedOpenAISSEReadCloser{
		segments: [][]byte{
			[]byte(`data: {"id":"chatcmpl_disconnect","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"one"},"finish_reason":null}]}` + "\n\n"),
			[]byte(`data: {"id":"chatcmpl_disconnect","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}` + "\n\n" + "data: [DONE]\n\n"),
		},
		gates: []<-chan struct{}{gate, nil},
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: streamBody}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := forceChatResponsesFallbackAccount()
	account.Platform = PlatformNvidia
	stop := StartOpenAIResponsesSSEKeepalive(ginCtx, 10*time.Millisecond)
	defer stop()
	resultCh := make(chan *OpenAIForwardResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := svc.Forward(context.Background(), ginCtx, account, []byte(`{"model":"nvidia/test","input":"hello","stream":true}`))
		resultCh <- result
		errCh <- err
	}()
	waitForFallbackRecorderBody(t, recorder, ": keepalive\n\n")
	cancel()
	close(gate)
	result := <-resultCh
	require.NoError(t, <-errCh)
	require.NotNil(t, result)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
}

type delayedNvidiaHTTPUpstream struct {
	entered chan struct{}
	release <-chan struct{}
	resp    *http.Response
	err     error
	requests int
	once    sync.Once
}

func (u *delayedNvidiaHTTPUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.requests++
	u.once.Do(func() { close(u.entered) })
	if u.release != nil {
		<-u.release
	}
	return u.resp, u.err
}

func (u *delayedNvidiaHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func TestForwardResponses_NvidiaKeepaliveCoversUpstreamHeaderWait(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := newOpenAIResponseFlushRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	release := make(chan struct{})
	upstream := &delayedNvidiaHTTPUpstream{
		entered: make(chan struct{}),
		release: release,
		resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(
			"data: {\"id\":\"chatcmpl_header\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n" +
				"data: {\"id\":\"chatcmpl_header\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n" +
				"data: [DONE]\n\n",
		))},
	}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := forceChatResponsesFallbackAccount()
	account.Platform = PlatformNvidia
	stop := StartOpenAIResponsesSSEKeepalive(c, 10*time.Millisecond)
	defer stop()
	resultCh := make(chan *OpenAIForwardResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"nvidia/test","input":"hello","stream":true}`))
		resultCh <- result
		errCh <- err
	}()
	waitForFallbackGate(t, upstream.entered)
	body := waitForFallbackRecorderBody(t, recorder, ": keepalive\n\n")
	require.NotContains(t, body, "response.output_text.delta")
	close(release)
	require.NoError(t, <-errCh)
	require.NotNil(t, <-resultCh)
	body, _ = recorder.snapshot()
	require.Contains(t, body, "response.completed")
}

func TestForwardResponses_NvidiaHTTPErrorBeforeKeepalivePreservesStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusGatewayTimeout,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_nim_504"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"NIM timed out"}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := forceChatResponsesFallbackAccount()
	account.Platform = PlatformNvidia
	stop := StartOpenAIResponsesSSEKeepalive(c, time.Hour)
	defer stop()

	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"nvidia/test","input":"hello","stream":true}`))
	var failoverErr *UpstreamFailoverError
	require.Error(t, err)
	require.False(t, errors.As(err, &failoverErr), "NVIDIA first-output errors must not trigger failover")
	require.Nil(t, result)
	require.Equal(t, http.StatusGatewayTimeout, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"type":"upstream_error"`)
	require.Contains(t, recorder.Body.String(), "NIM timed out")
	require.NotContains(t, recorder.Body.String(), "response.failed")
	require.Len(t, upstream.requests, 1)
}

func TestForwardResponses_NvidiaHTTPErrorAfterKeepaliveWritesSingleFailedEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := newOpenAIResponseFlushRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	release := make(chan struct{})
	upstream := &delayedNvidiaHTTPUpstream{
		entered: make(chan struct{}),
		release: release,
		resp: &http.Response{
			StatusCode: http.StatusGatewayTimeout,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_nim_504_stream"}},
			Body: io.NopCloser(strings.NewReader(
				`{"error":{"message":"NIM timed out after headers"}}`,
			)),
		},
	}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := forceChatResponsesFallbackAccount()
	account.Platform = PlatformNvidia
	stop := StartOpenAIResponsesSSEKeepalive(c, 10*time.Millisecond)
	defer stop()

	resultCh := make(chan *OpenAIForwardResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"nvidia/test","input":"hello","stream":true}`))
		resultCh <- result
		errCh <- err
	}()
	waitForFallbackGate(t, upstream.entered)
	waitForFallbackRecorderBody(t, recorder, ": keepalive\n\n")
	close(release)

	require.Nil(t, <-resultCh)
	require.Error(t, <-errCh)
	body, _ := recorder.snapshot()
	require.Equal(t, http.StatusOK, recorder.status)
	require.Equal(t, 1, strings.Count(body, `"type":"response.failed"`))
	require.Contains(t, body, "NIM timed out after headers")
	require.NotContains(t, body, "data: [DONE]")
	require.Equal(t, 1, upstream.requests)
}

func TestForwardResponses_NvidiaTransportErrorDoesNotFailoverOrWriteAfterDisconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := newOpenAIResponseFlushRecorder()
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestCtx)
	release := make(chan struct{})
	upstream := &delayedNvidiaHTTPUpstream{
		entered: make(chan struct{}),
		release: release,
		err:     errors.New("dial tcp: i/o timeout"),
	}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := forceChatResponsesFallbackAccount()
	account.Platform = PlatformNvidia
	stop := StartOpenAIResponsesSSEKeepalive(c, 10*time.Millisecond)
	defer stop()

	resultCh := make(chan *OpenAIForwardResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"nvidia/test","input":"hello","stream":true}`))
		resultCh <- result
		errCh <- err
	}()
	waitForFallbackGate(t, upstream.entered)
	waitForFallbackRecorderBody(t, recorder, ": keepalive\n\n")
	cancel()
	close(release)

	require.Nil(t, <-resultCh)
	err := <-errCh
	var failoverErr *UpstreamFailoverError
	require.Error(t, err)
	require.False(t, errors.As(err, &failoverErr), "transport errors while waiting for first output must not fail over")
	body, _ := recorder.snapshot()
	require.NotContains(t, body, "response.failed")
	require.Equal(t, 1, upstream.requests)
}

func TestOpenAIResponsesKeepaliveErrorSwitchesFromJSONToFailedSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := newOpenAIResponseFlushRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	stop := StartOpenAIResponsesSSEKeepalive(c, 10*time.Millisecond)
	defer stop()
	waitForFallbackRecorderBody(t, recorder, ": keepalive\n\n")
	writeOpenAIResponsesFallbackError(c, http.StatusBadGateway, "upstream_error", "upstream exploded")
	body, _ := recorder.snapshot()
	require.Equal(t, http.StatusOK, recorder.status)
	require.Equal(t, 1, strings.Count(body, `"type":"response.failed"`))
	require.Contains(t, body, "upstream exploded")
	require.NotContains(t, body, `"error":{"type"`)
}

func TestOpenAIResponsesKeepaliveBeforeFirstBeatPreservesJSONStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := newOpenAIResponseFlushRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	stop := StartOpenAIResponsesSSEKeepalive(c, time.Hour)
	defer stop()
	writeOpenAIResponsesFallbackError(c, http.StatusBadGateway, "upstream_error", "fast fail")
	body, _ := recorder.snapshot()
	require.Equal(t, http.StatusBadGateway, recorder.status)
	require.Contains(t, body, `"error":`)
	require.Contains(t, body, `"type":"upstream_error"`)
	require.NotContains(t, body, "response.failed")
}
