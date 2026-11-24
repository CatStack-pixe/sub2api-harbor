package apicompat

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Non-streaming: ResponsesResponse → AnthropicResponse
// ---------------------------------------------------------------------------

// ResponsesToAnthropic converts a Responses API response directly into an
// Anthropic Messages response. Reasoning output items are mapped to thinking
// blocks; function_call items become tool_use blocks.
func ResponsesToAnthropic(resp *ResponsesResponse, model string) *AnthropicResponse {
	out := &AnthropicResponse{
		ID:    resp.ID,
		Type:  "message",
		Role:  "assistant",
		Model: model,
		Usage: anthropicUsageFromResponsesUsage(nil),
	}

	var blocks []AnthropicContentBlock

	for _, item := range resp.Output {
		switch item.Type {
		case "reasoning":
			reasoningText := responsesReasoningText(item)
			// Always surface encrypted_content as thinking.signature so Claude
			// Code / multi-turn clients can send it back. Signature-only
			// thinking blocks are valid when the model omits a visible summary.
			if reasoningText != "" || strings.TrimSpace(item.EncryptedContent) != "" {
				blocks = append(blocks, AnthropicContentBlock{
					Type:      "thinking",
					Thinking:  reasoningText,
					Signature: item.EncryptedContent,
				})
			}
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" && part.Text != "" {
					blocks = append(blocks, AnthropicContentBlock{
						Type: "text",
						Text: part.Text,
					})
				}
			}
		case "function_call":
			blocks = append(blocks, AnthropicContentBlock{
				Type:  "tool_use",
				ID:    fromResponsesCallID(item.CallID),
				Name:  item.Name,
				Input: anthropicToolUseInput(item.Type, item.Name, item.Arguments),
			})
		case "custom_tool_call":
			if !resp.RenderCustomToolCalls || (resp.XAIServerSideSearch && IsServerSideSearchCall(item.Type, item.CallID)) {
				continue
			}
			blocks = append(blocks, AnthropicContentBlock{
				Type:  "tool_use",
				ID:    fromResponsesCallID(item.CallID),
				Name:  item.Name,
				Input: anthropicToolUseInput(item.Type, item.Name, item.Input),
			})
		case "tool_search_call":
			blocks = append(blocks, toolSearchCallToolUse(item))
		case "web_search_call":
			if item.Status != "completed" {
				continue
			}
			toolUseID := "srvtoolu_" + item.ID
			query := ""
			if item.Action != nil {
				query = item.Action.Query
			}
			inputJSON, _ := json.Marshal(map[string]string{"query": query})
			blocks = append(blocks, AnthropicContentBlock{
				Type:  "server_tool_use",
				ID:    toolUseID,
				Name:  "web_search",
				Input: inputJSON,
			})
			blocks = append(blocks, AnthropicContentBlock{
				Type:      "web_search_tool_result",
				ToolUseID: toolUseID,
				Content:   webSearchCallResults(&item),
			})
		}
	}

	if len(blocks) == 0 {
		blocks = append(blocks, AnthropicContentBlock{Type: "text", Text: ""})
	}
	out.Content = blocks

	out.StopReason = AnthropicStopReasonPtr(responsesStatusToAnthropicStopReason(resp.Status, resp.IncompleteDetails, blocks))

	if resp.Usage != nil {
		out.Usage = anthropicUsageFromResponsesUsage(resp.Usage)
	}

	return out
}

func anthropicUsageFromResponsesUsage(usage *ResponsesUsage) AnthropicUsage {
	if usage == nil {
		return AnthropicUsage{CacheCreationInputTokensPresent: true, CacheReadInputTokensPresent: true}
	}

	cachedTokens := 0
	if usage.InputTokensDetails != nil {
		cachedTokens = usage.InputTokensDetails.CachedTokens
	}

	inputTokens := usage.InputTokens - cachedTokens - usage.CacheCreationInputTokens
	if inputTokens < 0 {
		inputTokens = 0
	}

	return AnthropicUsage{
		InputTokens:                     inputTokens,
		OutputTokens:                    usage.OutputTokens,
		CacheReadInputTokens:            cachedTokens,
		CacheCreationInputTokens:        usage.CacheCreationInputTokens,
		CacheCreationInputTokensPresent: true,
		CacheReadInputTokensPresent:     true,
	}
}

func responsesStatusToAnthropicStopReason(status string, details *ResponsesIncompleteDetails, blocks []AnthropicContentBlock) string {
	switch status {
	case "incomplete":
		if details != nil && details.Reason == "max_output_tokens" {
			return "max_tokens"
		}
		return "end_turn"
	case "completed":
		if containsAnthropicToolUseBlock(blocks) {
			return "tool_use"
		}
		return "end_turn"
	default:
		return "end_turn"
	}
}

func containsAnthropicToolUseBlock(blocks []AnthropicContentBlock) bool {
	for _, block := range blocks {
		if block.Type == "tool_use" {
			return true
		}
	}
	return false
}

// anthropicToolUseInput returns the input of a non-streaming tool_use block,
// which must be a JSON object. An object is kept as it is. A function_call whose
// arguments are not an object (truncated by max_output_tokens, or empty) gets {};
// a custom_tool_call's free-form input is wrapped in the same {"input": ...}
// shape that downgrades a custom tool to a function tool, so that
// extractCustomToolCallInput gives the text back; an empty custom input is {} as
// it is in the streamed tool_use.
func anthropicToolUseInput(itemType, name, raw string) json.RawMessage {
	if trimmed := strings.TrimSpace(raw); strings.HasPrefix(trimmed, "{") && json.Valid([]byte(trimmed)) {
		return sanitizeAnthropicToolUseInput(name, raw)
	}
	if itemType == "custom_tool_call" && raw != "" {
		wrapped, err := json.Marshal(map[string]string{"input": raw})
		if err == nil {
			return wrapped
		}
	}
	return json.RawMessage("{}")
}

func sanitizeAnthropicToolUseInput(name string, raw string) json.RawMessage {
	if name != "Read" || raw == "" {
		return json.RawMessage(raw)
	}

	var input map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return json.RawMessage(raw)
	}

	if pages, ok := input["pages"]; !ok || string(pages) != `""` {
		return json.RawMessage(raw)
	}

	delete(input, "pages")
	sanitized, err := json.Marshal(input)
	if err != nil {
		return json.RawMessage(raw)
	}
	return sanitized
}

// ---------------------------------------------------------------------------
// Streaming: ResponsesStreamEvent → []AnthropicStreamEvent (stateful converter)
// ---------------------------------------------------------------------------

// responsesTextPart identifies one output_text part of a streamed response.
type responsesTextPart struct {
	OutputIndex  int
	ContentIndex int
}

// ResponsesEventToAnthropicState tracks state for converting a sequence of
// Responses SSE events directly into Anthropic SSE events.
type responsesAnthropicToolBlockState struct {
	OutputIndex         int
	BlockIndex          int
	CallID              string
	Name                string
	Arguments           string
	ArgumentsBuffer     *strings.Builder
	ArgumentsValidation *streamStringBytes
	HadDelta            bool
	Open                bool
	StopSent            bool
}

type ResponsesEventToAnthropicState struct {
	MessageStartSent bool
	MessageStopSent  bool

	// ContentBlockIndex is the next Anthropic content block index to allocate.
	ContentBlockIndex   int
	ContentBlockOpen    bool
	CurrentBlockIndex   int
	CurrentOutputIndex  int
	CurrentBlockType    string // "text" | "thinking" | "tool_use"
	CurrentToolName     string
	CurrentToolArgs     string
	CurrentToolHadDelta bool
	// These mirrors preserve the legacy active-tool accumulation state. They
	// are also the copy-on-write boundary when callers shallow-copy the stream
	// state while a Read tool is being validated.
	currentToolArgsBuffer     *strings.Builder
	currentToolArgsValidation *streamStringBytes
	// PendingThinkingSignature is filled from reasoning.encrypted_content and
	// emitted as signature_delta before the thinking block is closed.
	PendingThinkingSignature string
	HasToolCall              bool

	// XAIServerSideSearch is set by callers whose upstream is xAI. A
	// custom_tool_call that IsServerSideSearchCall then never becomes a tool_use
	// block, because xAI runs it itself.
	XAIServerSideSearch bool

	// OutputIndexToBlockIdx maps Responses output_index → Anthropic content block index.
	OutputIndexToBlockIdx map[int]int
	// Tool state is kept independently because Responses can interleave calls
	// before their arguments.done events arrive.
	toolBlocksByOutput map[int]*responsesAnthropicToolBlockState
	toolOutputByCallID map[string]int

	// textByPart records the text already delivered for each output_text part
	// so that a done payload can be reconciled against it. It outlives the
	// content block; closeCurrentBlock must not reset it.
	textByPart map[responsesTextPart]*strings.Builder
	// textDelivered records whether any assistant text reached the client.
	textDelivered bool
	// Text waits for active tools to finish so recovery never closes a tool
	// before its remaining arguments arrive or overlaps text with tool blocks.
	pendingTextEvents []ResponsesStreamEvent

	InputTokens              int
	OutputTokens             int
	CacheReadInputTokens     int
	CacheCreationInputTokens int

	ResponseID string
	Model      string
	Created    int64
}

// NewResponsesEventToAnthropicState returns an initialised stream state.
func NewResponsesEventToAnthropicState() *ResponsesEventToAnthropicState {
	return &ResponsesEventToAnthropicState{
		OutputIndexToBlockIdx: make(map[int]int),
		toolBlocksByOutput:    make(map[int]*responsesAnthropicToolBlockState),
		toolOutputByCallID:    make(map[string]int),
		textByPart:            make(map[responsesTextPart]*strings.Builder),
		Created:               time.Now().Unix(),
	}
}

// ResponsesEventToAnthropicEvents converts a single Responses SSE event into
// zero or more Anthropic SSE events, updating state as it goes.
func ResponsesEventToAnthropicEvents(
	evt *ResponsesStreamEvent,
	state *ResponsesEventToAnthropicState,
) []AnthropicStreamEvent {
	switch evt.Type {
	case "response.created":
		return resToAnthHandleCreated(evt, state)
	case "response.output_item.added":
		return resToAnthHandleOutputItemAdded(evt, state)
	case "response.output_text.delta", "response.output_text.done":
		return resToAnthHandleTextEvent(evt, state)
	case "response.function_call_arguments.delta",
		// custom/freeform 工具的输入增量与 function_call 参数增量同形。
		"response.custom_tool_call_input.delta":
		return resToAnthHandleFuncArgsDelta(evt, state)
	case "response.function_call_arguments.done", "response.custom_tool_call_input.done":
		events := resToAnthHandleFuncArgsDone(evt, state)
		return append(events, resToAnthFlushPendingText(state)...)
	case "response.output_item.done":
		events := resToAnthHandleOutputItemDone(evt, state)
		return append(events, resToAnthFlushPendingText(state)...)
	case "response.reasoning_summary_text.delta",
		// 原始推理文本增量，与 reasoning summary 一样映射为 thinking。
		"response.reasoning_text.delta":
		return resToAnthHandleReasoningDelta(evt, state)
	case "response.reasoning_summary_text.done":
		// Keep the thinking block open until response.output_item.done.
		// Grok/Codex attach encrypted_content on the finished reasoning item;
		// closing early would drop signature_delta and break multi-turn cache.
		return nil
	// response.done 是 Realtime/WS 与项目透传路径使用的终止别名；
	// 普通 Responses HTTP SSE 的公开终止事件仍以 response.completed 为主。
	case "response.completed", "response.done", "response.incomplete", "response.failed":
		return resToAnthHandleCompleted(evt, state)
	default:
		return nil
	}
}

// FinalizeResponsesAnthropicStream emits synthetic termination events if the
// stream ended without a proper completion event.
func FinalizeResponsesAnthropicStream(state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if !state.MessageStartSent || state.MessageStopSent {
		return nil
	}

	var events []AnthropicStreamEvent
	events = append(events, closeAllOpenBlocks(state)...)
	events = append(events, resToAnthFlushPendingText(state)...)
	events = append(events, closeCurrentNonToolBlock(state)...)

	stopReason := "end_turn"
	if state.HasToolCall {
		stopReason = "tool_use"
	}

	events = append(events,
		AnthropicStreamEvent{
			Type: "message_delta",
			Delta: &AnthropicDelta{
				StopReason: stopReason,
			},
			Usage: &AnthropicUsage{
				InputTokens:                     state.InputTokens,
				OutputTokens:                    state.OutputTokens,
				CacheReadInputTokens:            state.CacheReadInputTokens,
				CacheCreationInputTokens:        state.CacheCreationInputTokens,
				CacheCreationInputTokensPresent: true,
				CacheReadInputTokensPresent:     true,
			},
		},
		AnthropicStreamEvent{Type: "message_stop"},
	)
	state.MessageStopSent = true
	return events
}

// ResponsesAnthropicEventToSSE formats an AnthropicStreamEvent as an SSE line pair.
func ResponsesAnthropicEventToSSE(evt AnthropicStreamEvent) (string, error) {
	data, err := json.Marshal(evt)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("event: %s\ndata: %s\n\n", evt.Type, data), nil
}

// --- internal handlers ---

func resToAnthHandleCreated(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if evt.Response != nil {
		state.ResponseID = evt.Response.ID
		// Only use upstream model if no override was set (e.g. originalModel)
		if state.Model == "" {
			state.Model = evt.Response.Model
		}
	}

	if state.MessageStartSent {
		return nil
	}
	state.MessageStartSent = true

	// Official Anthropic message_start uses stop_reason: null and usage with
	// input_tokens when known. We leave StopReason nil (JSON null) and usage
	// zeros until response.completed; never emit stop_reason:"" which breaks
	// strict clients' turn-finalization / session usage accounting.
	return []AnthropicStreamEvent{{
		Type: "message_start",
		Message: &AnthropicResponse{
			ID:         state.ResponseID,
			Type:       "message",
			Role:       "assistant",
			Content:    []AnthropicContentBlock{},
			Model:      state.Model,
			StopReason: nil,
			Usage: AnthropicUsage{
				InputTokens:                     0,
				OutputTokens:                    0,
				CacheCreationInputTokensPresent: true,
				CacheReadInputTokensPresent:     true,
			},
		},
	}}
}

func resToAnthHandleOutputItemAdded(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if evt.Item == nil {
		return nil
	}

	switch evt.Item.Type {
	// function_call 与 custom_tool_call（custom/freeform 工具，如新版 apply_patch）
	// 同样映射为 Anthropic 的 tool_use 块。
	case "function_call", "custom_tool_call":
		state.ensureToolBlockMaps()
		if existing := state.findToolBlock(evt); existing != nil {
			return nil
		}
		if state.XAIServerSideSearch && IsServerSideSearchCall(evt.Item.Type, evt.Item.CallID) {
			return nil
		}

		events := closeCurrentNonToolBlock(state)
		idx := allocateAnthropicContentBlockIndex(state)
		arguments := evt.Item.Arguments
		if evt.Item.Type == "custom_tool_call" {
			arguments = evt.Item.Input
		}
		tool := &responsesAnthropicToolBlockState{
			OutputIndex: evt.OutputIndex,
			BlockIndex:  idx,
			CallID:      evt.Item.CallID,
			Name:        evt.Item.Name,
			Arguments:   arguments,
			Open:        true,
		}
		state.OutputIndexToBlockIdx[evt.OutputIndex] = idx
		state.toolBlocksByOutput[evt.OutputIndex] = tool
		if tool.CallID != "" {
			state.toolOutputByCallID[tool.CallID] = evt.OutputIndex
		}
		state.mirrorToolBlock(tool)
		state.HasToolCall = true

		events = append(events, AnthropicStreamEvent{
			Type:  "content_block_start",
			Index: &idx,
			ContentBlock: &AnthropicContentBlock{
				Type:  "tool_use",
				ID:    fromResponsesCallID(evt.Item.CallID),
				Name:  evt.Item.Name,
				Input: json.RawMessage("{}"),
			},
		})
		return events

	case "reasoning":
		events := closeCurrentNonToolBlock(state)

		idx := allocateAnthropicContentBlockIndex(state)
		state.OutputIndexToBlockIdx[evt.OutputIndex] = idx
		state.ContentBlockOpen = true
		state.CurrentBlockIndex = idx
		state.CurrentOutputIndex = evt.OutputIndex
		state.CurrentBlockType = "thinking"
		state.PendingThinkingSignature = strings.TrimSpace(evt.Item.EncryptedContent)

		events = append(events, AnthropicStreamEvent{
			Type:  "content_block_start",
			Index: &idx,
			ContentBlock: &AnthropicContentBlock{
				Type:     "thinking",
				Thinking: "",
			},
		})
		return events

	case "message":
		return nil
	}

	return nil
}

func resToAnthHandleTextEvent(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if state.MessageStopSent {
		return nil
	}
	if state.hasOpenToolBlocks() {
		state.pendingTextEvents = append(state.pendingTextEvents, *evt)
		return nil
	}
	if evt.Type == "response.output_text.done" {
		return resToAnthHandleTextDone(evt, state)
	}
	return resToAnthHandleTextDelta(evt, state)
}

func (state *ResponsesEventToAnthropicState) hasOpenToolBlocks() bool {
	if state.ContentBlockOpen && state.CurrentBlockType == "tool_use" {
		return true
	}
	for _, tool := range state.toolBlocksByOutput {
		if tool != nil && tool.Open && !tool.StopSent {
			return true
		}
	}
	return false
}

func resToAnthFlushPendingText(state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if len(state.pendingTextEvents) == 0 || state.hasOpenToolBlocks() {
		return nil
	}
	pending := state.pendingTextEvents
	state.pendingTextEvents = nil
	var events []AnthropicStreamEvent
	for i := range pending {
		events = append(events, resToAnthHandleTextEvent(&pending[i], state)...)
	}
	return events
}

func resToAnthHandleTextDelta(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	return resToAnthEmitText(evt.Delta, resToAnthTextPartOf(evt), state)
}

func resToAnthTextPartOf(evt *ResponsesStreamEvent) responsesTextPart {
	return responsesTextPart{OutputIndex: evt.OutputIndex, ContentIndex: evt.ContentIndex}
}

// resToAnthEmitText opens a text block when needed, emits text, and records it
// against its part so that a later payload for the same part is reconciled
// against what the client already received.
func resToAnthEmitText(text string, part responsesTextPart, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if text == "" {
		return nil
	}

	var events []AnthropicStreamEvent

	if !state.ContentBlockOpen || state.CurrentBlockType != "text" {
		events = append(events, closeCurrentNonToolBlock(state)...)

		idx := allocateAnthropicContentBlockIndex(state)
		state.OutputIndexToBlockIdx[part.OutputIndex] = idx
		state.ContentBlockOpen = true
		state.CurrentBlockIndex = idx
		state.CurrentOutputIndex = part.OutputIndex
		state.CurrentBlockType = "text"

		events = append(events, AnthropicStreamEvent{
			Type:  "content_block_start",
			Index: &idx,
			ContentBlock: &AnthropicContentBlock{
				Type: "text",
				Text: "",
			},
		})
	}

	delivered, ok := state.textByPart[part]
	if !ok {
		delivered = &strings.Builder{}
		state.textByPart[part] = delivered
	}
	_, _ = delivered.WriteString(text)
	state.textDelivered = true

	idx := state.CurrentBlockIndex
	events = append(events, AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: &AnthropicDelta{
			Type: "text_delta",
			Text: text,
		},
	})
	return events
}

// resToAnthRecoverText emits the tail of a finished text payload that never
// reached the client. Streamed text cannot be recalled, so a payload that does
// not extend what was already delivered is left alone.
func resToAnthRecoverText(text string, part responsesTextPart, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	builder, known := state.textByPart[part]
	if !known && state.textDelivered {
		// The payload is indexed differently from every delta seen so far, so
		// which part it finishes cannot be established. Recovering it could
		// repeat an answer the client already has, which is worse than leaving
		// a partially delivered one alone.
		return nil
	}

	var delivered string
	if known {
		delivered = builder.String()
	}
	if text == delivered || !strings.HasPrefix(text, delivered) {
		return nil
	}
	return resToAnthEmitText(text[len(delivered):], part, state)
}

// resToAnthHandleTextDone recovers text that upstream carried only on the done
// event before closing the block, which some streams use instead of sending
// output_text.delta at all.
func resToAnthHandleTextDone(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if state.MessageStopSent {
		// The message is already terminated; a late payload cannot be delivered
		// without emitting a content block after message_stop.
		return resToAnthHandleBlockDone(evt, state)
	}

	events := resToAnthRecoverText(evt.Text, resToAnthTextPartOf(evt), state)
	return append(events, resToAnthHandleBlockDone(evt, state)...)
}

func resToAnthHandleFuncArgsDelta(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if evt.Delta == "" {
		return nil
	}

	tool := state.writableToolBlock(evt)
	if tool == nil || !tool.Open || tool.StopSent {
		return nil
	}

	previousArgs := tool.Arguments
	if state.isMirroredToolBlock(tool) {
		previousArgs = state.CurrentToolArgs
		tool.ArgumentsBuffer = state.currentToolArgsBuffer
		tool.ArgumentsValidation = state.currentToolArgsValidation
	}
	tool.Arguments = appendStreamString(&tool.ArgumentsBuffer, previousArgs, evt.Delta)
	state.mirrorToolBlock(tool)

	if tool.Name == "Read" {
		if tool.HadDelta {
			return nil
		}
		validatedArgs := appendStreamStringBytes(&tool.ArgumentsValidation, previousArgs, tool.Arguments, evt.Delta)
		state.mirrorToolValidation(tool)
		if !json.Valid(validatedArgs) {
			return nil
		}

		tool.HadDelta = true
		state.mirrorToolBlock(tool)
		sanitized := sanitizeAnthropicToolUseInput(tool.Name, tool.Arguments)
		return []AnthropicStreamEvent{{
			Type:  "content_block_delta",
			Index: &tool.BlockIndex,
			Delta: &AnthropicDelta{
				Type:        "input_json_delta",
				PartialJSON: string(sanitized),
			},
		}}
	}

	tool.HadDelta = true
	state.mirrorToolBlock(tool)

	return []AnthropicStreamEvent{{
		Type:  "content_block_delta",
		Index: &tool.BlockIndex,
		Delta: &AnthropicDelta{
			Type:        "input_json_delta",
			PartialJSON: evt.Delta,
		},
	}}
}

func resToAnthHandleFuncArgsDone(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	tool := state.writableToolBlock(evt)
	if tool == nil || !tool.Open || tool.StopSent {
		return nil
	}
	raw := evt.Arguments
	if raw == "" {
		raw = evt.Input
	}
	if raw == "" {
		raw = tool.Arguments
	}
	return emitToolArgumentsAndClose(state, tool, raw)
}

func resToAnthHandleReasoningDelta(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if evt.Delta == "" {
		return nil
	}

	blockIdx, ok := state.OutputIndexToBlockIdx[evt.OutputIndex]
	if !ok {
		return nil
	}

	return []AnthropicStreamEvent{{
		Type:  "content_block_delta",
		Index: &blockIdx,
		Delta: &AnthropicDelta{
			Type:     "thinking_delta",
			Thinking: evt.Delta,
		},
	}}
}

func resToAnthHandleBlockDone(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if !state.ContentBlockOpen || state.CurrentBlockType != "text" {
		return nil
	}
	if blockIdx, ok := state.OutputIndexToBlockIdx[evt.OutputIndex]; ok && blockIdx != state.CurrentBlockIndex {
		return nil
	}
	return closeCurrentBlock(state)
}

func resToAnthHandleOutputItemDone(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if evt.Item == nil {
		return nil
	}

	// Handle web_search_call → synthesize server_tool_use + web_search_tool_result blocks.
	if evt.Item.Type == "web_search_call" && evt.Item.Status == "completed" {
		return resToAnthHandleWebSearchDone(evt, state)
	}
	// The native tool_search_call carries its arguments only on done.
	if evt.Item.Type == "tool_search_call" {
		return resToAnthHandleToolSearchDone(evt, state)
	}

	switch evt.Item.Type {
	case "function_call", "custom_tool_call":
		tool := state.writableToolBlock(evt)
		if tool == nil || !tool.Open || tool.StopSent {
			return nil
		}
		raw := evt.Item.Arguments
		if evt.Item.Type == "custom_tool_call" {
			raw = evt.Item.Input
		}
		if raw == "" {
			raw = tool.Arguments
		}
		return emitToolArgumentsAndClose(state, tool, raw)

	case "reasoning":
		// Capture encrypted_content on reasoning item done (often only present here).
		if sig := strings.TrimSpace(evt.Item.EncryptedContent); sig != "" {
			state.PendingThinkingSignature = sig
		}
		return closeCurrentOutputBlock(evt, state, "thinking")

	case "message":
		return closeCurrentOutputBlock(evt, state, "text")
	}

	return nil
}

// resToAnthHandleWebSearchDone converts an OpenAI web_search_call output item
// into Anthropic server_tool_use + web_search_tool_result content block pairs.
// This allows Claude Code to count the searches performed.
func resToAnthHandleWebSearchDone(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	var events []AnthropicStreamEvent
	events = append(events, closeCurrentNonToolBlock(state)...)

	toolUseID := "srvtoolu_" + evt.Item.ID
	query := ""
	if evt.Item.Action != nil {
		query = evt.Item.Action.Query
	}
	inputJSON, _ := json.Marshal(map[string]string{"query": query})

	// Emit server_tool_use block (start + stop).
	idx1 := allocateAnthropicContentBlockIndex(state)
	events = append(events, AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx1,
		ContentBlock: &AnthropicContentBlock{
			Type:  "server_tool_use",
			ID:    toolUseID,
			Name:  "web_search",
			Input: inputJSON,
		},
	})
	events = append(events, AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: &idx1,
	})

	// Emit web_search_tool_result block (start + stop).
	// Use result metadata when available; some providers expose no result details.
	idx2 := allocateAnthropicContentBlockIndex(state)
	events = append(events, AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx2,
		ContentBlock: &AnthropicContentBlock{
			Type:      "web_search_tool_result",
			ToolUseID: toolUseID,
			Content:   webSearchCallResults(evt.Item),
		},
	})
	events = append(events, AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: &idx2,
	})
	return events
}

// resToAnthRecoverTerminalText emits assistant text that only ever appeared in
// the terminal response payload, which some streams populate without sending
// any output_text event.
//
// It only runs when no text at all reached the client. Streamed events and the
// terminal output array carry no guaranteed common identity, so reconciling
// them part by part risks repeating an answer the client already has, which is
// worse than leaving a partially streamed response as it is.
func resToAnthRecoverTerminalText(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if state.textDelivered || evt.Response == nil {
		return nil
	}

	var events []AnthropicStreamEvent
	for outputIndex, item := range evt.Response.Output {
		if item.Type != "message" {
			continue
		}
		for contentIndex, content := range item.Content {
			if content.Type != "output_text" {
				continue
			}
			part := responsesTextPart{OutputIndex: outputIndex, ContentIndex: contentIndex}
			events = append(events, resToAnthEmitText(content.Text, part, state)...)
		}
	}
	if len(events) > 0 {
		events = append(events, closeCurrentBlock(state)...)
	}
	return events
}

func resToAnthHandleCompleted(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if state.MessageStopSent {
		return nil
	}

	var events []AnthropicStreamEvent
	events = append(events, closeAllOpenBlocks(state)...)
	events = append(events, resToAnthFlushPendingText(state)...)
	events = append(events, closeCurrentNonToolBlock(state)...)
	events = append(events, resToAnthRecoverTerminalText(evt, state)...)

	stopReason := "end_turn"
	if evt.Usage != nil {
		usage := anthropicUsageFromResponsesUsage(evt.Usage)
		state.InputTokens = usage.InputTokens
		state.OutputTokens = usage.OutputTokens
		state.CacheReadInputTokens = usage.CacheReadInputTokens
		state.CacheCreationInputTokens = usage.CacheCreationInputTokens
	}
	if evt.Response != nil {
		if evt.Response.Usage != nil {
			usage := anthropicUsageFromResponsesUsage(evt.Response.Usage)
			state.InputTokens = usage.InputTokens
			state.OutputTokens = usage.OutputTokens
			state.CacheReadInputTokens = usage.CacheReadInputTokens
			state.CacheCreationInputTokens = usage.CacheCreationInputTokens
		}
		switch evt.Response.Status {
		case "incomplete":
			if evt.Response.IncompleteDetails != nil && evt.Response.IncompleteDetails.Reason == "max_output_tokens" {
				stopReason = "max_tokens"
			}
		case "completed":
			if state.HasToolCall {
				stopReason = "tool_use"
			}
		}
	}

	events = append(events,
		AnthropicStreamEvent{
			Type: "message_delta",
			Delta: &AnthropicDelta{
				StopReason: stopReason,
			},
			Usage: &AnthropicUsage{
				InputTokens:                     state.InputTokens,
				OutputTokens:                    state.OutputTokens,
				CacheReadInputTokens:            state.CacheReadInputTokens,
				CacheCreationInputTokens:        state.CacheCreationInputTokens,
				CacheCreationInputTokensPresent: true,
				CacheReadInputTokensPresent:     true,
			},
		},
		AnthropicStreamEvent{Type: "message_stop"},
	)
	state.MessageStopSent = true
	return events
}

func closeCurrentBlock(state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if !state.ContentBlockOpen {
		return nil
	}
	if state.CurrentBlockType == "tool_use" {
		tool := state.writableToolBlock(&ResponsesStreamEvent{OutputIndex: state.CurrentOutputIndex})
		return closeToolBlock(state, tool)
	}

	idx := state.CurrentBlockIndex
	var events []AnthropicStreamEvent
	// Emit signature_delta before stop so Claude clients retain encrypted
	// reasoning for the next turn (required for Grok multi-turn cache).
	if state.CurrentBlockType == "thinking" {
		if sig := strings.TrimSpace(state.PendingThinkingSignature); sig != "" {
			events = append(events, AnthropicStreamEvent{
				Type:  "content_block_delta",
				Index: &idx,
				Delta: &AnthropicDelta{
					Type:      "signature_delta",
					Signature: sig,
				},
			})
		}
		state.PendingThinkingSignature = ""
	}
	state.ContentBlockOpen = false
	state.CurrentBlockType = ""
	state.CurrentBlockIndex = 0
	state.CurrentOutputIndex = 0
	state.CurrentToolName = ""
	state.CurrentToolArgs = ""
	state.CurrentToolHadDelta = false
	events = append(events, AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: &idx,
	})
	return events
}

func allocateAnthropicContentBlockIndex(state *ResponsesEventToAnthropicState) int {
	idx := state.ContentBlockIndex
	state.ContentBlockIndex++
	return idx
}

func closeCurrentNonToolBlock(state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if !state.ContentBlockOpen || state.CurrentBlockType == "tool_use" {
		return nil
	}
	return closeCurrentBlock(state)
}

func closeCurrentOutputBlock(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState, blockType string) []AnthropicStreamEvent {
	if !state.ContentBlockOpen || state.CurrentBlockType != blockType {
		return nil
	}
	if blockIdx, ok := state.OutputIndexToBlockIdx[evt.OutputIndex]; ok && blockIdx != state.CurrentBlockIndex {
		return nil
	}
	return closeCurrentBlock(state)
}

func (state *ResponsesEventToAnthropicState) ensureToolBlockMaps() {
	if state.OutputIndexToBlockIdx == nil {
		state.OutputIndexToBlockIdx = make(map[int]int)
	}
	if state.toolBlocksByOutput == nil {
		state.toolBlocksByOutput = make(map[int]*responsesAnthropicToolBlockState)
	}
	if state.toolOutputByCallID == nil {
		state.toolOutputByCallID = make(map[string]int)
	}
}

func (state *ResponsesEventToAnthropicState) findToolBlock(evt *ResponsesStreamEvent) *responsesAnthropicToolBlockState {
	state.ensureToolBlockMaps()
	callID := evt.CallID
	if evt.Item != nil && evt.Item.CallID != "" {
		callID = evt.Item.CallID
	}
	if callID != "" {
		if outputIndex, ok := state.toolOutputByCallID[callID]; ok {
			return state.toolBlocksByOutput[outputIndex]
		}
	}
	return state.toolBlocksByOutput[evt.OutputIndex]
}

func (state *ResponsesEventToAnthropicState) findOrCreateLegacyToolBlock(evt *ResponsesStreamEvent) *responsesAnthropicToolBlockState {
	if tool := state.findToolBlock(evt); tool != nil {
		return tool
	}
	if state.CurrentBlockType != "tool_use" {
		return nil
	}

	blockIdx, ok := state.OutputIndexToBlockIdx[evt.OutputIndex]
	if !ok {
		blockIdx = state.CurrentBlockIndex
	}
	name := evt.Name
	callID := evt.CallID
	if evt.Item != nil {
		if name == "" {
			name = evt.Item.Name
		}
		if callID == "" {
			callID = evt.Item.CallID
		}
	}
	if name == "" {
		name = state.CurrentToolName
	}

	tool := &responsesAnthropicToolBlockState{
		OutputIndex:         evt.OutputIndex,
		BlockIndex:          blockIdx,
		CallID:              callID,
		Name:                name,
		Arguments:           state.CurrentToolArgs,
		ArgumentsBuffer:     state.currentToolArgsBuffer,
		ArgumentsValidation: state.currentToolArgsValidation,
		HadDelta:            state.CurrentToolHadDelta,
		Open:                true,
	}
	blocks := make(map[int]*responsesAnthropicToolBlockState, len(state.toolBlocksByOutput)+1)
	for outputIndex, existing := range state.toolBlocksByOutput {
		blocks[outputIndex] = existing
	}
	blocks[evt.OutputIndex] = tool
	state.toolBlocksByOutput = blocks

	blockIndices := make(map[int]int, len(state.OutputIndexToBlockIdx)+1)
	for outputIndex, existing := range state.OutputIndexToBlockIdx {
		blockIndices[outputIndex] = existing
	}
	blockIndices[evt.OutputIndex] = blockIdx
	state.OutputIndexToBlockIdx = blockIndices
	if callID != "" {
		outputByCallID := make(map[string]int, len(state.toolOutputByCallID)+1)
		for existingCallID, outputIndex := range state.toolOutputByCallID {
			outputByCallID[existingCallID] = outputIndex
		}
		outputByCallID[callID] = evt.OutputIndex
		state.toolOutputByCallID = outputByCallID
	}
	if state.ContentBlockIndex <= blockIdx {
		state.ContentBlockIndex = blockIdx + 1
	}
	return tool
}

// writableToolBlock gives this state its own tool record and map before a
// mutation. A shallow copy may share the maps and records, while its public
// active-tool fields remain independent and are the source of truth for the
// currently mirrored tool.
func (state *ResponsesEventToAnthropicState) writableToolBlock(evt *ResponsesStreamEvent) *responsesAnthropicToolBlockState {
	tool := state.findOrCreateLegacyToolBlock(evt)
	if tool == nil {
		return nil
	}

	copyOfTool := *tool
	if state.isMirroredToolBlock(tool) {
		copyOfTool.Arguments = state.CurrentToolArgs
		copyOfTool.ArgumentsBuffer = state.currentToolArgsBuffer
		copyOfTool.ArgumentsValidation = state.currentToolArgsValidation
		copyOfTool.HadDelta = state.CurrentToolHadDelta
	}
	blocks := make(map[int]*responsesAnthropicToolBlockState, len(state.toolBlocksByOutput))
	for outputIndex, existing := range state.toolBlocksByOutput {
		blocks[outputIndex] = existing
	}
	blocks[copyOfTool.OutputIndex] = &copyOfTool
	state.toolBlocksByOutput = blocks
	return &copyOfTool
}

func (state *ResponsesEventToAnthropicState) isMirroredToolBlock(tool *responsesAnthropicToolBlockState) bool {
	return tool != nil &&
		state.CurrentBlockType == "tool_use" &&
		state.CurrentBlockIndex == tool.BlockIndex &&
		state.CurrentOutputIndex == tool.OutputIndex
}

func (state *ResponsesEventToAnthropicState) mirrorToolBlock(tool *responsesAnthropicToolBlockState) {
	if tool == nil || (state.ContentBlockOpen && state.CurrentBlockType != "tool_use") {
		return
	}
	state.ContentBlockOpen = tool.Open
	state.CurrentBlockIndex = tool.BlockIndex
	state.CurrentOutputIndex = tool.OutputIndex
	state.CurrentBlockType = "tool_use"
	state.CurrentToolName = tool.Name
	state.CurrentToolArgs = tool.Arguments
	state.CurrentToolHadDelta = tool.HadDelta
	state.currentToolArgsBuffer = tool.ArgumentsBuffer
	state.currentToolArgsValidation = tool.ArgumentsValidation
}

func (state *ResponsesEventToAnthropicState) mirrorToolValidation(tool *responsesAnthropicToolBlockState) {
	if !state.isMirroredToolBlock(tool) {
		return
	}
	state.currentToolArgsValidation = tool.ArgumentsValidation
}

func emitToolArgumentsAndClose(
	state *ResponsesEventToAnthropicState,
	tool *responsesAnthropicToolBlockState,
	raw string,
) []AnthropicStreamEvent {
	if tool == nil || !tool.Open || tool.StopSent {
		return nil
	}

	var events []AnthropicStreamEvent
	if !tool.HadDelta && raw != "" {
		if tool.Name == "Read" {
			raw = string(sanitizeAnthropicToolUseInput(tool.Name, raw))
		}
		if raw != "" {
			events = append(events, AnthropicStreamEvent{
				Type:  "content_block_delta",
				Index: &tool.BlockIndex,
				Delta: &AnthropicDelta{
					Type:        "input_json_delta",
					PartialJSON: raw,
				},
			})
			tool.Arguments = raw
			tool.HadDelta = true
			state.mirrorToolBlock(tool)
		}
	} else if tool.HadDelta && tool.Name != "Read" && strings.HasPrefix(raw, tool.Arguments) && len(raw) > len(tool.Arguments) {
		remaining := raw[len(tool.Arguments):]
		events = append(events, AnthropicStreamEvent{
			Type:  "content_block_delta",
			Index: &tool.BlockIndex,
			Delta: &AnthropicDelta{
				Type:        "input_json_delta",
				PartialJSON: remaining,
			},
		})
		tool.Arguments = raw
	}
	state.mirrorToolBlock(tool)
	events = append(events, closeToolBlock(state, tool)...)
	return events
}

func closeToolBlock(state *ResponsesEventToAnthropicState, tool *responsesAnthropicToolBlockState) []AnthropicStreamEvent {
	if tool == nil || !tool.Open || tool.StopSent {
		return nil
	}
	tool.Open = false
	tool.StopSent = true
	if state.CurrentBlockType == "tool_use" && state.CurrentBlockIndex == tool.BlockIndex {
		state.ContentBlockOpen = false
		state.CurrentBlockType = ""
		state.CurrentBlockIndex = 0
		state.CurrentOutputIndex = 0
		state.CurrentToolName = ""
		state.CurrentToolArgs = ""
		state.CurrentToolHadDelta = false
		state.currentToolArgsBuffer = nil
		state.currentToolArgsValidation = nil
	}
	tool.ArgumentsBuffer = nil
	tool.ArgumentsValidation = nil
	idx := tool.BlockIndex
	return []AnthropicStreamEvent{{Type: "content_block_stop", Index: &idx}}
}

func closeAllOpenBlocks(state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	state.ensureToolBlockMaps()
	if state.ContentBlockOpen && state.CurrentBlockType == "tool_use" {
		state.findOrCreateLegacyToolBlock(&ResponsesStreamEvent{OutputIndex: state.CurrentOutputIndex})
	}

	type openBlock struct {
		index       int
		outputIndex int
		callID      string
		tool        *responsesAnthropicToolBlockState
		serial      bool
	}
	blocks := make([]openBlock, 0, len(state.toolBlocksByOutput)+1)
	seenTools := make(map[*responsesAnthropicToolBlockState]bool)
	if state.ContentBlockOpen && state.CurrentBlockType != "tool_use" {
		blocks = append(blocks, openBlock{index: state.CurrentBlockIndex, serial: true})
	} else if state.ContentBlockOpen && state.CurrentBlockType == "tool_use" {
		if tool := state.writableToolBlock(&ResponsesStreamEvent{OutputIndex: state.CurrentOutputIndex}); tool != nil {
			seenTools[tool] = true
			blocks = append(blocks, openBlock{
				index:       tool.BlockIndex,
				outputIndex: tool.OutputIndex,
				callID:      tool.CallID,
				tool:        tool,
			})
		}
	}
	for _, tool := range state.toolBlocksByOutput {
		if tool == nil || !tool.Open || tool.StopSent || seenTools[tool] {
			continue
		}
		seenTools[tool] = true
		blocks = append(blocks, openBlock{
			index:       tool.BlockIndex,
			outputIndex: tool.OutputIndex,
			callID:      tool.CallID,
			tool:        tool,
		})
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].index < blocks[j].index })

	var events []AnthropicStreamEvent
	for _, block := range blocks {
		if block.serial {
			events = append(events, closeCurrentBlock(state)...)
			continue
		}
		tool := state.writableToolBlock(&ResponsesStreamEvent{
			OutputIndex: block.outputIndex,
			CallID:      block.callID,
		})
		events = append(events, closeToolBlock(state, tool)...)
	}
	return events
}
