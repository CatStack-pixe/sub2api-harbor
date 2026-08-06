package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// forwardResponsesViaRawChatCompletions serves /v1/responses clients through an
// upstream that only supports /v1/chat/completions.
func (s *OpenAIGatewayService) forwardResponsesViaRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &responsesReq); err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("parse responses request: %w", err)
	}
	originalModel := strings.TrimSpace(responsesReq.Model)
	if originalModel == "" {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}

	clientStream := responsesReq.Stream
	serviceTier := extractOpenAIServiceTierFromBody(body)
	// custom 工具（如 codex 的 exec）降级为 function 工具转发，回程需按名字还原为
	// custom_tool_call 项，先记下名字集合；tool_search 工具同理，回程还原为
	// tool_search_call 项；namespace 子工具（如 MCP 工具）摊平转发，回程按映射还原
	// 为带 namespace 字段的 function_call 项。
	effectiveTools, err := apicompat.EffectiveResponsesTools(&responsesReq)
	if err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("resolve responses tools: %w", err)
	}
	customTools := apicompat.CustomToolNames(effectiveTools)
	toolSearch := apicompat.HasToolSearchTool(effectiveTools)
	namespaceTools := apicompat.NamespaceToolNames(effectiveTools)

	chatReq, err := apicompat.ResponsesToChatCompletionsRequest(&responsesReq)
	if err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("convert responses to chat completions: %w", err)
	}

	billingModel := resolveOpenAIForwardModel(account, originalModel, "")
	if fallbackModel, fallbackActive := s.resolveAgnesQuotaFallbackModel(account, billingModel, time.Now()); fallbackActive {
		billingModel = fallbackModel
	}
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)
	// 国产模型默认 effort 补充：需要 mappedModel 判定，推迟到 billingModel 算出之后。
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, billingModel)
	chatReq.Model = upstreamModel
	if clientStream {
		chatReq.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	}

	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat completions fallback request: %w", err)
	}
	chatBody, err = s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, chatBody)
	if err != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(err, &blocked) {
			writeOpenAIFastPolicyBlockedResponse(c, blocked)
		}
		return nil, err
	}
	if serviceTier == nil {
		serviceTier = extractOpenAIServiceTierFromBody(chatBody)
	}

	logger.L().Debug("openai responses: forwarding via raw chat completions",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", clientStream),
	)

	// Build and send upstream request via the shared CC pipeline
	apiKey, targetURL, err := s.resolveCCFallbackTarget(account)
	if err != nil {
		return nil, err
	}
	resp, err := s.sendCCUpstreamRequest(ctx, c, account, targetURL, chatBody, clientStream, apiKey, account.GetOpenAIUserAgent(), "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, upstreamMsg := s.readOpenAIUpstreamError(resp)
		if s.activateAgnesQuotaFallback(account, upstreamModel, resp.StatusCode, respBody, time.Now()) {
			return s.forwardResponsesViaRawChatCompletions(ctx, c, account, body)
		}
		if foErr := s.failoverOpenAIUpstreamHTTPError(ctx, c, account, resp, respBody, upstreamMsg, upstreamModel); foErr != nil {
			return nil, foErr
		}
		return s.handleErrorResponse(ctx, resp, c, account, chatBody, billingModel)
	}

	if clientStream {
		return s.streamChatCompletionsAsResponses(c, resp, originalModel, customTools, toolSearch, namespaceTools, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	return s.bufferChatCompletionsAsResponses(c, resp, originalModel, customTools, toolSearch, namespaceTools, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
}

func (s *OpenAIGatewayService) bufferChatCompletionsAsResponses(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	customTools map[string]bool,
	toolSearch bool,
	namespaceTools map[string]apicompat.NamespacedToolName,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	ccResp, usage, err := s.readCCUpstreamJSONResponse(c, resp, writeOpenAIResponsesFallbackError)
	if err != nil {
		return nil, err
	}
	responsesResp := apicompat.ChatCompletionsResponseToResponses(ccResp, originalModel, customTools, toolSearch, namespaceTools)

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.JSON(http.StatusOK, responsesResp)

	return &OpenAIForwardResult{
		RequestID:       requestID,
		Usage:           usage,
		Model:           originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: reasoningEffort,
		ServiceTier:     serviceTier,
		Stream:          false,
		Duration:        time.Since(startTime),
	}, nil
}

func (s *OpenAIGatewayService) streamChatCompletionsAsResponses(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	customTools map[string]bool,
	toolSearch bool,
	namespaceTools map[string]apicompat.NamespacedToolName,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	keepaliveOwner, _, lastDownstreamWriteAt, keepaliveInterval := takeOverOpenAIResponsesSSEKeepalive(c)
	writeInitialStreamHeaders := s.newStreamHeaderWriter(c, resp.Header)
	writeStreamHeaders := func() {
		if keepaliveOwner != nil && keepaliveOwner.committed() {
			return
		}
		writeInitialStreamHeaders()
	}

	state := apicompat.NewChatCompletionsToResponsesStreamState(originalModel)
	state.CustomTools = customTools
	state.ToolSearchDeclared = toolSearch
	state.NamespaceTools = namespaceTools
	clientDisconnected := false
	downstreamWritten := func() {}

	writeEvents := func(events []apicompat.ResponsesStreamEvent) {
		if clientDisconnected || len(events) == 0 {
			return
		}
		wroteEvent := false
		writeStreamHeaders()
		for _, event := range events {
			sse, err := apicompat.ResponsesEventToSSE(event)
			if err != nil {
				logger.L().Warn("openai responses chat fallback: failed to marshal stream event",
					zap.Error(err),
					zap.String("request_id", requestID),
				)
				continue
			}
			if _, err := fmt.Fprint(c.Writer, sse); err != nil {
				clientDisconnected = true
				logger.L().Debug("openai responses chat fallback: client disconnected, continuing to drain upstream for billing",
					zap.Error(err),
					zap.String("request_id", requestID),
				)
				return
			}
			wroteEvent = true
		}
		if wroteEvent {
			c.Writer.Flush()
			downstreamWritten()
		}
	}

	resultWithScan := func(scan ccStreamScanState) *OpenAIForwardResult {
		return &OpenAIForwardResult{
			RequestID:       requestID,
			Usage:           scan.Usage,
			Model:           originalModel,
			BillingModel:    billingModel,
			UpstreamModel:   upstreamModel,
			ReasoningEffort: reasoningEffort,
			ServiceTier:     serviceTier,
			Stream:          true,
			Duration:        time.Since(startTime),
			FirstTokenMs:    scan.FirstTokenMs,
		}
	}

	finishStream := func(scan ccStreamScanState) (*OpenAIForwardResult, error) {
		if scan.Err != nil {
			return resultWithScan(scan), fmt.Errorf("stream usage incomplete: %w", scan.Err)
		}

		writeEvents(apicompat.FinalizeChatCompletionsResponsesStream(state))
		if !clientDisconnected {
			writeStreamHeaders()
			if _, err := fmt.Fprint(c.Writer, "data: [DONE]\n\n"); err != nil {
				clientDisconnected = true
			}
			if !clientDisconnected {
				c.Writer.Flush()
				downstreamWritten()
			}
		}
		if !scan.SawDone {
			logCCStreamMissingDoneSentinel("openai responses chat fallback", requestID)
		}
		return resultWithScan(scan), nil
	}

	if keepaliveOwner == nil || keepaliveInterval <= 0 {
		scan := s.scanCCStream(resp, "openai responses chat fallback", requestID, startTime, func(chunk *apicompat.ChatCompletionsChunk) {
			writeEvents(apicompat.ChatCompletionsChunkToResponsesEvents(chunk, state))
		})
		return finishStream(scan)
	}

	if lastDownstreamWriteAt.IsZero() {
		lastDownstreamWriteAt = time.Now()
	}
	firstTickAfter := keepaliveInterval - time.Since(lastDownstreamWriteAt)
	if firstTickAfter <= 0 {
		firstTickAfter = time.Nanosecond
	}
	keepaliveTicker := time.NewTicker(firstTickAfter)
	defer keepaliveTicker.Stop()
	keepaliveCh := keepaliveTicker.C
	downstreamWritten = func() {
		lastDownstreamWriteAt = time.Now()
		keepaliveTicker.Reset(keepaliveInterval)
	}

	chunks := make(chan *apicompat.ChatCompletionsChunk)
	scanDone := make(chan ccStreamScanState, 1)
	go func() {
		scan := s.scanCCStream(resp, "openai responses chat fallback", requestID, startTime, func(chunk *apicompat.ChatCompletionsChunk) {
			chunks <- chunk
		})
		scanDone <- scan
	}()

	var clientDone <-chan struct{}
	if c.Request != nil {
		clientDone = c.Request.Context().Done()
		if c.Request.Context().Err() != nil {
			clientDisconnected = true
			clientDone = nil
			keepaliveCh = nil
		}
	}

	for {
		select {
		case chunk := <-chunks:
			if !clientDisconnected && c.Request != nil && c.Request.Context().Err() != nil {
				clientDisconnected = true
				clientDone = nil
				keepaliveCh = nil
			}
			writeEvents(apicompat.ChatCompletionsChunkToResponsesEvents(chunk, state))
		case scan := <-scanDone:
			return finishStream(scan)
		case <-clientDone:
			clientDisconnected = true
			clientDone = nil
			keepaliveCh = nil
		case <-keepaliveCh:
			if clientDisconnected || (c.Request != nil && c.Request.Context().Err() != nil) {
				clientDisconnected = true
				clientDone = nil
				keepaliveCh = nil
				continue
			}
			if !keepaliveOwner.beat() {
				clientDisconnected = true
				keepaliveCh = nil
				logger.L().Debug("openai responses chat fallback: client disconnected during keepalive, continuing to drain upstream for billing",
					zap.String("request_id", requestID),
				)
				continue
			}
			downstreamWritten()
		}
	}
}

func chatChunkStartsResponsesOutput(chunk *apicompat.ChatCompletionsChunk) bool {
	if chunk == nil {
		return false
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != nil || choice.Delta.ReasoningContent != nil || len(choice.Delta.ToolCalls) > 0 {
			return true
		}
	}
	return false
}
