package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

// openaiCCRawAllowedHeaders 是 CC 直转路径专用的客户端 header 透传白名单。
//
// **关键**：不能复用 openaiAllowedHeaders——后者含 Codex 客户端专属 header
// （originator / session_id / x-codex-turn-state / x-codex-turn-metadata / conversation_id），
// 这些在 ChatGPT OAuth 上游是必需的，但透传给 DeepSeek/Kimi/GLM 等第三方
// OpenAI 兼容上游会造成：
//   - 完全忽略（多数友好厂商）——隐性污染上游统计
//   - 400 "unknown parameter"（严格上游）——可见错误
//
// 这里仅放行通用 HTTP header；content-type / authorization / accept 由上下文
// 显式设置，不依赖透传。
//
// 参见决策记录：
// pensieve/short-term/maxims/dont-reuse-shared-headers-whitelist-across-different-upstream-trust-domains
var openaiCCRawAllowedHeaders = map[string]bool{
	"accept-language": true,
	"user-agent":      true,
}

// forwardAsRawChatCompletions 直转客户端的 Chat Completions 请求到上游
// `{base_url}/v1/chat/completions`，**不**做 CC↔Responses 协议转换。
//
// 适用场景：account.platform=openai && account.type=apikey && 上游已被探测确认
// 不支持 /v1/responses 端点（如 GLM/Qwen 等第三方 OpenAI 兼容上游）；CN 供应商
// 固定 chat_completions 协议也走此路径。
//
// 与 ForwardAsChatCompletions 的关键差异：
//
//   - 不调用 apicompat.ChatCompletionsToResponses，body 仅做模型 ID 改写
//   - 上游 URL 拼到 /v1/chat/completions 而非 /v1/responses
//   - 流式响应 SSE 直接透传给客户端（上游 chunk 已是 CC 格式）
//   - 非流式响应 JSON 直接透传，仅按需提取 usage
//   - 不应用 codex OAuth transform（APIKey 路径无 OAuth）
//   - 不注入 prompt_cache_key（OAuth 专属机制）
//
// 调用入口：openai_gateway_chat_completions.go::ForwardAsChatCompletions
// 在函数顶部按 openai_compat.ShouldUseResponsesAPI 分流。
func (s *OpenAIGatewayService) forwardAsRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	// 1. Parse minimal fields needed for routing/billing
	originalModel := gjson.GetBytes(body, "model").String()
	if originalModel == "" {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	clientStream := gjson.GetBytes(body, "stream").Bool()

	// 2. Resolve model mapping (same as ForwardAsChatCompletions)
	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	if fallbackModel, fallbackActive := s.resolveAgnesQuotaFallbackModel(account, billingModel, time.Now()); fallbackActive {
		billingModel = fallbackModel
	}
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	SetOpsUpstreamModel(c, upstreamModel)
	grokCacheIdentity := ""
	if account.Platform == PlatformGrok {
		// Resolve before image bridging or other body rewrites so the fallback is
		// anchored to the client's stable conversation prefix.
		grokCacheIdentity = resolveGrokCacheIdentity(c, body, "", upstreamModel)
	}
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)
	// 国产模型默认 effort 补充：需要 mappedModel 判定，推迟到 billingModel 算出之后。
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, billingModel)

	// 3. Rewrite model in body (no protocol conversion)
	upstreamBody := body
	if upstreamModel != originalModel {
		upstreamBody = ReplaceModelInBody(body, upstreamModel)
	}
	if shouldNormalizeCNChatCompletionsStop(account) {
		normalizedBody, changed, normalizeErr := normalizeCNChatCompletionsStopField(upstreamBody)
		if normalizeErr != nil {
			return nil, fmt.Errorf("normalize CN stop field: %w", normalizeErr)
		}
		if changed {
			upstreamBody = normalizedBody
		}
	}
	if normalizedBody, normalized := NormalizeGLMOpenAIReasoningEffort(upstreamBody, upstreamModel); normalized {
		upstreamBody = normalizedBody
	}
	if account.Platform == PlatformGLM {
		normalizedBody, changed, normalizeErr := normalizeGLMChatCompletionsRequestBody(upstreamBody)
		if normalizeErr != nil {
			return nil, fmt.Errorf("normalize GLM chat request: %w", normalizeErr)
		}
		if changed {
			upstreamBody = normalizedBody
		}
	}
	if deepSeekTextOnlyImageRequest(account, upstreamModel, upstreamBody) {
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalModelConfiguration)
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", deepSeekTextOnlyImageInputMessage)
		return nil, errors.New(deepSeekTextOnlyImageInputMessage)
	}

	// 4. Apply OpenAI fast policy on the CC body
	updatedBody, policyErr := s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, upstreamBody)
	if policyErr != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(policyErr, &blocked) {
			MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
			writeChatCompletionsError(c, http.StatusForbidden, "permission_error", blocked.Message)
		}
		return nil, policyErr
	}
	upstreamBody = updatedBody
	// 计费兜底 tier = 最终出站 body（policy filter/force 后）里的 tier；
	// 最终值由 resolvedOpenAIUpstreamServiceTier 决定（上游回显优先）。
	serviceTier := extractOpenAIServiceTierFromBody(upstreamBody)
	if account.Platform == PlatformGrok {
		strippedBody, stripErr := stripRedundantGrokChatViewImageTool(upstreamBody)
		if stripErr != nil {
			return nil, fmt.Errorf("strip redundant Grok Chat view_image tool: %w", stripErr)
		}
		upstreamBody = strippedBody
	}

	// Grok Composer does not accept image_url parts directly, but Grok Build
	// can describe the images first. Bridge only this exact failure mode.
	token, tokenKind, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("account %d missing %s credential", account.ID, tokenKind)
	}

	var bridgeUsage OpenAIUsage
	if account.Platform == PlatformGrok {
		bridgedBody, usage, bridged, bridgeErr := s.bridgeGrokComposerImageInputs(ctx, c, account, upstreamBody, token)
		if bridgeErr != nil {
			var failoverErr *UpstreamFailoverError
			if !errors.As(bridgeErr, &failoverErr) && c != nil && c.Writer != nil && !c.Writer.Written() {
				writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", bridgeErr.Error())
			}
			return nil, bridgeErr
		}
		if bridged {
			upstreamBody = bridgedBody
			addOpenAIUsage(&bridgeUsage, usage)
		}
	}

	if clientStream {
		var usageErr error
		upstreamBody, usageErr = ensureOpenAIChatStreamUsage(upstreamBody)
		if usageErr != nil {
			return nil, fmt.Errorf("enable stream usage: %w", usageErr)
		}
	}
	if account.Platform == PlatformGrok {
		upstreamBody, err = stripGrokChatPromptCacheKey(upstreamBody)
		if err != nil {
			return nil, fmt.Errorf("remove Responses-only Grok prompt cache key: %w", err)
		}
		upstreamBody, err = normalizeGrokChatReasoningEffort(upstreamBody, upstreamModel)
		if err != nil {
			return nil, fmt.Errorf("normalize Grok chat reasoning effort: %w", err)
		}
	}
	upstreamBody = applyOllamaCloudRawChatCompletionsRequest(account, upstreamBody)

	logger.L().Debug("openai chat_completions raw: forwarding without protocol conversion",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", clientStream),
	)

	// 5. Build and send upstream request via the shared CC pipeline
	targetURL, err := s.rawChatCompletionsURL(account)
	if err != nil {
		return nil, err
	}
	SetActualOpenAIUpstreamEndpoint(c, grokChatRawEndpoint)
	customUA := account.GetOpenAIUserAgent()
	if customUA == "" && account.IsGrokOAuth() {
		customUA = defaultGrokUpstreamUserAgent()
	}
	resp, err := s.sendCCUpstreamRequest(ctx, c, account, targetURL, upstreamBody, clientStream, token, customUA, grokCacheIdentity)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// 7. Handle error response with failover
	if resp.StatusCode >= 400 {
		respBody, upstreamMsg := s.readOpenAIUpstreamError(resp)
		if s.activateAgnesQuotaFallback(account, upstreamModel, resp.StatusCode, respBody, time.Now()) {
			return s.forwardAsRawChatCompletions(ctx, c, account, body, defaultMappedModel)
		}
		if account.Platform == PlatformGrok {
			kind := "http_error"
			if s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody) {
				kind = "failover"
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id")),
				Kind:               kind,
				Message:            upstreamMsg,
			})
			s.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(ctx, upstreamModel), account, resp.StatusCode, resp.Header, respBody)
			if s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody) {
				retryable, retryDelay, retryDeadline, retryMax := grokSameAccountRetryMetadata(account, resp.StatusCode, respBody)
				return nil, &UpstreamFailoverError{
					StatusCode:               resp.StatusCode,
					ResponseBody:             respBody,
					ResponseHeaders:          resp.Header.Clone(),
					RetryableOnSameAccount:   retryable,
					RequestScopedTransient:   retryable && resp.StatusCode == http.StatusTooManyRequests,
					SameAccountRetryDelay:    retryDelay,
					SameAccountRetryDeadline: retryDeadline,
					SameAccountRetryMax:      retryMax,
				}
			}
			return s.handleChatCompletionsErrorResponse(resp, c, account, billingModel)
		}
		if foErr := s.failoverOpenAIUpstreamHTTPError(ctx, c, account, resp, respBody, upstreamMsg, upstreamModel); foErr != nil {
			return nil, foErr
		}
		return s.handleChatCompletionsErrorResponse(resp, c, account, billingModel)
	}

	if account.Platform == PlatformGrok {
		s.updateGrokUsageFromResponse(withGrokTeamRateLimitModel(ctx, upstreamModel), account, resp.Header, resp.StatusCode)
	}

	// 8. Forward response
	var result *OpenAIForwardResult
	var forwardErr error
	if clientStream {
		result, forwardErr = s.streamRawChatCompletions(c, resp, account, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime, len(body))
	} else {
		result, forwardErr = s.bufferRawChatCompletions(c, resp, account, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	if result != nil {
		addOpenAIUsage(&result.Usage, bridgeUsage)
		result.UpstreamEndpoint = grokChatRawEndpoint
	}
	return result, forwardErr
}

func (s *OpenAIGatewayService) rawChatCompletionsURL(account *Account) (string, error) {
	if account.Platform == PlatformGrok {
		targetURL, err := buildGrokChatCompletionsURL(account, s.cfg, s.settingService)
		if err != nil {
			return "", fmt.Errorf("invalid grok base_url: %w", err)
		}
		return targetURL, nil
	}

	return s.openAIChatCompletionsTargetURL(account)
}

// rawChatFirstOutputTimeout returns the optional watchdog for native Chat
// Completions streams. It is intentionally separate from the Responses
// first-output setting so existing OpenAI Responses behavior is unchanged.
func (s *OpenAIGatewayService) rawChatFirstOutputTimeout() time.Duration {
	if s == nil || s.cfg == nil || s.cfg.Gateway.OpenAIRawChatFirstOutputTimeoutSeconds <= 0 {
		return 0
	}
	return time.Duration(s.cfg.Gateway.OpenAIRawChatFirstOutputTimeoutSeconds) * time.Second
}

// streamRawChatCompletions 透传上游 CC SSE 流到客户端，并提取 usage（包括
// 末尾 [DONE] 之前的 chunk 中的 usage 字段，按 OpenAI CC 协议）。
//
// usage 字段仅在客户端请求 stream_options.include_usage=true 时出现于上游响应中。
// 网关会对上游强制打开 include_usage 以保证计费完整，并原样向下游透传 usage，
// 让级联代理或下游计费系统也能拿到完整用量。
func (s *OpenAIGatewayService) streamRawChatCompletions(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
	requestBodyLen int,
) (*OpenAIForwardResult, error) {
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	requestID := resp.Header.Get("x-request-id")
	writeStreamHeaders := s.newStreamHeaderWriter(c, resp.Header)
	scanner := s.newUpstreamSSEScanner(resp.Body)
	keepaliveStop := func() {}
	var keepaliveOwner *openAIResponsesSSEKeepaliveOwner
	var keepaliveInterval time.Duration
	if s.cfg != nil && s.cfg.Gateway.StreamKeepaliveInterval > 0 {
		keepaliveInterval = time.Duration(s.cfg.Gateway.StreamKeepaliveInterval) * time.Second
		if _, alreadyStarted := c.Get(openAIResponsesSSEKeepaliveKey); !alreadyStarted {
			keepaliveStop = StartOpenAIResponsesSSEKeepalive(c, keepaliveInterval)
			keepaliveOwner, _, _, _ = takeOverOpenAIResponsesSSEKeepalive(c)
		}
	}
	defer keepaliveStop()

	var usage OpenAIUsage
	var firstTokenMs *int
	clientDisconnected := false
	clientOutputStarted := false
	sawDone := false
	pendingLines := make([]string, 0, 8)
	refusalDetector := newOpenAIChatSilentRefusalDetector(requestBodyLen)

	writeLine := func(line string) {
		if clientDisconnected {
			return
		}
		if !clientOutputStarted && !refusalDetector.ShouldReleaseClientOutput() {
			pendingLines = append(pendingLines, line)
			return
		}
		if !clientOutputStarted {
			writeStreamHeaders()
			for _, pending := range pendingLines {
				if _, werr := c.Writer.WriteString(pending + "\n"); werr != nil {
					clientDisconnected = true
					logger.L().Debug("openai chat_completions raw: client disconnected, continuing to drain upstream for billing",
						zap.Error(werr),
						zap.String("request_id", requestID),
					)
					return
				}
			}
			pendingLines = pendingLines[:0]
			clientOutputStarted = true
		}
		if _, werr := c.Writer.WriteString(line + "\n"); werr != nil {
			clientDisconnected = true
			logger.L().Debug("openai chat_completions raw: client disconnected, continuing to drain upstream for billing",
				zap.Error(werr),
				zap.String("request_id", requestID),
			)
		}
	}

	processLine := func(line string) {
		refusalDetector.ObserveSSELine(line)
		if payload, ok := extractOpenAISSEDataLine(line); ok {
			trimmedPayload := strings.TrimSpace(payload)
			if trimmedPayload == "[DONE]" {
				sawDone = true
			} else {
				observer.ObserveOpenAI([]byte(payload), strings.TrimSpace(gjson.Get(payload, "type").String()))
				usageOnlyChunk := isOpenAIChatUsageOnlyStreamChunk(payload)
				if u := extractCCStreamUsage(payload); u != nil {
					usage = *u
				}
				if firstTokenMs == nil && !usageOnlyChunk {
					elapsed := int(time.Since(startTime).Milliseconds())
					firstTokenMs = &elapsed
				}
			}
		}
		line = applyOllamaCloudRawChatCompletionsSSELine(account, line)
		line = stripEmptyChatToolCallIdentityFromSSELine(line)

		writeLine(line)
		if line == "" {
			if !clientDisconnected && clientOutputStarted {
				c.Writer.Flush()
			}
			return
		}
		if !clientDisconnected && clientOutputStarted {
			c.Writer.Flush()
		}
	}

	// Scanner reads block on the upstream body. Always move it to a worker so
	// stream idle/first-output watchdogs can fire even when the upstream is
	// silent. Closing resp.Body on return unblocks the worker and prevents a
	// client disconnect from leaving an orphaned read goroutine.
	type scanEvent struct {
		line string
		err  error
	}
	events := make(chan scanEvent, 16)
	drainDone := make(chan struct{})
	sendEvent := func(event scanEvent) bool {
		select {
		case events <- event:
			return true
		case <-drainDone:
			return false
		}
	}
	go func() {
		defer close(events)
		for scanner.Scan() {
			if !sendEvent(scanEvent{line: scanner.Text()}) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			_ = sendEvent(scanEvent{err: err})
		}
	}()
	defer func() {
		close(drainDone)
		_ = resp.Body.Close()
	}()

	streamInterval := time.Duration(0)
	if s.cfg != nil && s.cfg.Gateway.StreamDataIntervalTimeout > 0 {
		streamInterval = time.Duration(s.cfg.Gateway.StreamDataIntervalTimeout) * time.Second
	}
	var intervalTicker *time.Ticker
	if streamInterval > 0 {
		intervalTicker = time.NewTicker(streamInterval)
		defer intervalTicker.Stop()
	}
	var intervalCh <-chan time.Time
	if intervalTicker != nil {
		intervalCh = intervalTicker.C
	}

	firstOutputTimeout := s.rawChatFirstOutputTimeout()
	var firstOutputTimer *time.Timer
	if firstOutputTimeout > 0 {
		remaining := time.Until(startTime.Add(firstOutputTimeout))
		if remaining <= 0 {
			remaining = time.Nanosecond
		}
		firstOutputTimer = time.NewTimer(remaining)
		defer firstOutputTimer.Stop()
	}
	var firstOutputCh <-chan time.Time
	if firstOutputTimer != nil {
		firstOutputCh = firstOutputTimer.C
	}

	// A canceled client must not cancel the detached upstream request. Mark the
	// client as disconnected and continue collecting usage, subject to the same
	// stream idle watchdog used for connected clients.
	var clientDone <-chan struct{}
	requestCtx := context.Background()
	if c != nil && c.Request != nil {
		requestCtx = c.Request.Context()
		clientDone = c.Request.Context().Done()
	}
	var keepaliveTicker *time.Ticker
	if keepaliveOwner != nil && keepaliveInterval > 0 {
		keepaliveTicker = time.NewTicker(keepaliveInterval)
		defer keepaliveTicker.Stop()
	}
	var keepaliveCh <-chan time.Time
	if keepaliveTicker != nil {
		keepaliveCh = keepaliveTicker.C
	}

	lastUpstreamAt := time.Now()
	var streamErr error
streamLoop:
	for {
		select {
		case event, ok := <-events:
			if !ok {
				streamErr = scanner.Err()
				break streamLoop
			}
			if event.err != nil {
				streamErr = event.err
				break streamLoop
			}
			lastUpstreamAt = time.Now()
			processLine(event.line)
			if firstOutputTimer != nil && firstTokenMs != nil {
				firstOutputTimer.Stop()
				firstOutputTimer = nil
				firstOutputCh = nil
			}
			if sawDone {
				break streamLoop
			}
		case <-clientDone:
			clientDisconnected = true
			clientDone = nil
			logger.L().Debug("openai chat_completions raw: client request canceled, continuing to drain upstream for billing",
				zap.String("request_id", requestID),
			)
		case <-firstOutputCh:
			if firstTokenMs != nil || sawDone {
				continue
			}
			if clientDisconnected {
				streamErr = fmt.Errorf("stream usage incomplete after first output timeout")
				break streamLoop
			}
			streamErr = s.newOpenAIFirstOutputTimeoutError(
				requestCtx, c, account, startTime, originalModel, "", firstOutputTimeout, "raw_chat_first_output", resp.Header,
			)
			break streamLoop
		case <-intervalCh:
			if time.Since(lastUpstreamAt) < streamInterval {
				continue
			}
			if clientDisconnected {
				streamErr = fmt.Errorf("stream usage incomplete after timeout")
				break streamLoop
			}
			if !clientOutputStarted {
				streamErr = s.newOpenAIFirstOutputTimeoutError(
					requestCtx, c, account, startTime, originalModel, "", streamInterval, "raw_chat_stream_interval", resp.Header,
				)
				break streamLoop
			}
			if s.rateLimitService != nil {
				s.rateLimitService.HandleStreamTimeout(requestCtx, account, upstreamModel)
			}
			logger.L().Warn("openai chat_completions raw: stream data interval timeout",
				zap.String("request_id", requestID),
				zap.Duration("interval", streamInterval),
			)
			streamErr = fmt.Errorf("stream data interval timeout")
			break streamLoop
		case <-keepaliveCh:
			if clientDisconnected {
				continue
			}
			if !keepaliveOwner.beat() {
				clientDisconnected = true
				logger.L().Debug("openai chat_completions raw: client disconnected during keepalive, continuing to drain upstream for billing",
					zap.String("request_id", requestID),
				)
			}
		}
	}

	if streamErr != nil && !errors.Is(streamErr, context.Canceled) && !errors.Is(streamErr, context.DeadlineExceeded) {
		logger.L().Warn("openai chat_completions raw: stream read error",
			zap.Error(streamErr),
			zap.String("request_id", requestID),
		)
	}

	resultWithUsage := func() *OpenAIForwardResult {
		return &OpenAIForwardResult{
			RequestID:                     requestID,
			Usage:                         usage,
			Model:                         originalModel,
			BillingModel:                  billingModel,
			UpstreamModel:                 upstreamModel,
			UpstreamResponseModel:         observedUpstreamResponseModel(c),
			UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
			ReasoningEffort:               reasoningEffort,
			ServiceTier:                   serviceTier,
			Stream:                        true,
			Duration:                      time.Since(startTime),
			FirstTokenMs:                  firstTokenMs,
		}
	}
	requestCanceled := c != nil && c.Request != nil && c.Request.Context().Err() != nil
	var streamFailoverErr *UpstreamFailoverError
	if streamErr != nil && errors.As(streamErr, &streamFailoverErr) {
		// A watchdog firing before any client bytes were committed is safe to
		// retry on another account. Do not manufacture a billable result.
		if clientDisconnected || requestCanceled {
			return resultWithUsage(), fmt.Errorf("stream usage incomplete after disconnect: %w", streamErr)
		}
		return nil, streamFailoverErr
	}

	// A client write failure must never be attributed to the selected proxy.
	// Keep draining the upstream above so usage accounting remains complete.
	if clientDisconnected || requestCanceled {
		if sawDone {
			s.clearOpenAIProxyStreamDisconnect(account)
			return resultWithUsage(), nil
		}
		if streamErr != nil {
			return resultWithUsage(), fmt.Errorf("stream usage incomplete after disconnect: %w", streamErr)
		}
		return resultWithUsage(), fmt.Errorf("stream usage incomplete after disconnect: missing terminal event")
	}

	if streamErr != nil {
		if errors.Is(streamErr, context.Canceled) || errors.Is(streamErr, context.DeadlineExceeded) {
			return resultWithUsage(), fmt.Errorf("stream usage incomplete: %w", streamErr)
		}
		if strings.Contains(streamErr.Error(), "stream data interval timeout") {
			if clientOutputStarted && !clientDisconnected {
				writeStreamHeaders()
				if _, werr := c.Writer.WriteString(buildChatStreamErrorSSE("upstream_stream_timeout", "Upstream stream data interval timeout")); werr == nil {
					_, _ = c.Writer.WriteString("data: [DONE]\n\n")
					c.Writer.Flush()
				}
			}
			return resultWithUsage(), streamErr
		}
		if !clientOutputStarted {
			s.recordOpenAIProxyStreamDisconnect(account, streamErr, requestID)
			return nil, s.newOpenAIStreamFailoverError(c, account, false, requestID, nil, "OpenAI stream disconnected before completion", resp.Header)
		}
		s.recordOpenAIProxyStreamDisconnect(account, streamErr, requestID)
		streamReadErr := newOpenAIUpstreamStreamReadError(streamErr)
		if code, message, ok := OpenAIUpstreamStreamReadErrorDetails(streamReadErr); ok {
			writeStreamHeaders()
			if _, werr := c.Writer.WriteString(buildChatStreamErrorSSE(code, message)); werr == nil {
				_, _ = c.Writer.WriteString("data: [DONE]\n\n")
				c.Writer.Flush()
			}
		}
		return resultWithUsage(), streamReadErr
	}

	if !sawDone {
		if !clientOutputStarted {
			s.recordOpenAIProxyStreamDisconnect(account, errors.New("stream ended before terminal event"), requestID)
			if refusalDetector.IsSilentRefusal() {
				return nil, newOpenAISilentRefusalFailoverError(c, account, requestID)
			}
			return nil, s.newOpenAIStreamFailoverError(c, account, false, requestID, nil, "OpenAI stream ended before a terminal event", resp.Header)
		}
		s.recordOpenAIProxyStreamDisconnect(account, errors.New("stream ended before terminal event"), requestID)
		writeStreamHeaders()
		if _, werr := c.Writer.WriteString(buildChatStreamErrorSSE(OpenAIUpstreamStreamReadErrorCode, "OpenAI stream ended before a terminal event")); werr == nil {
			_, _ = c.Writer.WriteString("data: [DONE]\n\n")
			c.Writer.Flush()
		}
		return resultWithUsage(), fmt.Errorf("stream usage incomplete: missing terminal event")
	}

	s.clearOpenAIProxyStreamDisconnect(account)
	if !clientOutputStarted {
		if refusalDetector.IsSilentRefusal() {
			return nil, newOpenAISilentRefusalFailoverError(c, account, requestID)
		}
		if len(pendingLines) > 0 {
			writeStreamHeaders()
			for _, pending := range pendingLines {
				if _, werr := c.Writer.WriteString(pending + "\n"); werr != nil {
					clientDisconnected = true
					logger.L().Debug("openai chat_completions raw: client disconnected during final flush",
						zap.Error(werr),
						zap.String("request_id", requestID),
					)
					break
				}
			}
			if !clientDisconnected {
				c.Writer.Flush()
				clientOutputStarted = true
			}
		}
	}

	return &OpenAIForwardResult{
		RequestID:                     requestID,
		Usage:                         usage,
		Model:                         originalModel,
		BillingModel:                  billingModel,
		UpstreamModel:                 upstreamModel,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		UpstreamResponseServiceTier:   observedUpstreamResponseServiceTier(c),
		ReasoningEffort:               reasoningEffort,
		ServiceTier:                   resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		Stream:                        true,
		Duration:                      time.Since(startTime),
		FirstTokenMs:                  firstTokenMs,
	}, nil
}

// ensureOpenAIChatStreamUsage 确保 raw Chat Completions 流式请求会让上游返回 usage。
// usage 也会继续向下游透传，支持级联代理和下游计费系统。
func ensureOpenAIChatStreamUsage(body []byte) ([]byte, error) {
	updated, err := sjson.SetBytes(body, "stream_options.include_usage", true)
	if err != nil {
		return body, err
	}
	return updated, nil
}

func isOpenAIChatUsageOnlyStreamChunk(payload string) bool {
	if strings.TrimSpace(payload) == "" {
		return false
	}
	if !gjson.Get(payload, "usage").Exists() {
		return false
	}
	choices := gjson.Get(payload, "choices")
	return choices.Exists() && choices.IsArray() && len(choices.Array()) == 0
}

// extractCCStreamUsage 从单个 CC 流式 chunk 的 payload 中提取 usage 字段。
// CC 协议中 usage 仅出现在末尾 chunk（且仅当 include_usage 生效时），
// 但上游可能在多个 chunk 中重复——总是用最新值。
func extractCCStreamUsage(payload string) *OpenAIUsage {
	usageResult := gjson.Get(payload, "usage")
	if !usageResult.Exists() || !usageResult.IsObject() {
		return nil
	}
	u, ok := openAIUsageFromGJSON(usageResult)
	if !ok {
		return nil
	}
	return &u
}

// bufferRawChatCompletions 透传上游 CC 非流式 JSON 响应。
func (s *OpenAIGatewayService) bufferRawChatCompletions(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")

	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeChatCompletionsError(c, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		}
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	observer.ObserveOpenAI(respBody, strings.TrimSpace(gjson.GetBytes(respBody, "type").String()))

	var usage OpenAIUsage
	if parsedUsage, ok := extractOpenAIUsageFromJSONBytes(respBody); ok {
		usage = parsedUsage
	}
	responseModel := gjson.GetBytes(respBody, "model").String()
	if requiresBillableGrokChatUsage(account, billingModel, upstreamModel, responseModel) && !hasBillableGrokChatUsage(usage) {
		upstreamRequestID := firstNonEmpty(requestID, resp.Header.Get("xai-request-id"))
		return nil, newGrokMissingUsageFailoverError(c, account, upstreamRequestID)
	}
	respBody = applyOllamaCloudRawChatCompletionsResponse(account, respBody)

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Writer.Header().Set("Content-Type", ct)
	} else {
		c.Writer.Header().Set("Content-Type", "application/json")
	}
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(respBody)

	return &OpenAIForwardResult{
		RequestID:                     requestID,
		Usage:                         usage,
		Model:                         originalModel,
		BillingModel:                  billingModel,
		UpstreamModel:                 upstreamModel,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		UpstreamResponseServiceTier:   observedUpstreamResponseServiceTier(c),
		ReasoningEffort:               reasoningEffort,
		ServiceTier:                   resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		Stream:                        false,
		Duration:                      time.Since(startTime),
	}, nil
}

// buildOpenAIChatCompletionsURL 拼接上游 Chat Completions 端点 URL。
//
//   - base 已是 /chat/completions：原样返回
//   - base 以 /v1 结尾：追加 /chat/completions
//   - base 以其他版本段结尾（如 /v4）：追加 /chat/completions
//   - 其他情况：追加 /v1/chat/completions
//
// 与 buildOpenAIResponsesURL 是姐妹函数。
func buildOpenAIChatCompletionsURL(base string) string {
	return buildOpenAIEndpointURL(base, "/v1/chat/completions")
}
