package service

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// normalizeGLMChatCompletionsRequestBody removes fields accepted by the
// OpenAI Responses surface but rejected by GLM's Chat Completions endpoint.
// GLM still receives the equivalent flat reasoning_effort value so a nested
// OpenAI reasoning object does not silently discard the caller's intent.
func normalizeGLMChatCompletionsRequestBody(body []byte) ([]byte, bool, error) {
	if len(body) == 0 || !gjson.ValidBytes(body) || !gjson.GetBytes(body, "model").Exists() {
		return body, false, nil
	}
	model := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "model").String()))
	if !strings.HasPrefix(model, "glm-") {
		return body, false, nil
	}

	updated := body
	changed := false
	for _, field := range []string{"prompt_cache_key", "promptCacheKey"} {
		if !gjson.GetBytes(updated, field).Exists() {
			continue
		}
		next, err := sjson.DeleteBytes(updated, field)
		if err != nil {
			return body, false, fmt.Errorf("delete GLM unsupported field %s: %w", field, err)
		}
		updated = next
		changed = true
	}

	// NormalizeGLMOpenAIReasoningEffort handles the usual nested/flat mapping.
	// Copy the nested value to the flat field before deleting the unsupported
	// nested key; this also handles a caller that supplied only reasoning.effort.
	if normalized, normalizedChanged := NormalizeGLMOpenAIReasoningEffort(updated, model); normalizedChanged {
		updated = normalized
		changed = true
	}
	reasoning := gjson.GetBytes(updated, "reasoning")
	if reasoning.IsObject() && reasoning.Get("effort").Exists() {
		effort := normalizeGLMOpenAIReasoningEffort(reasoning.Get("effort").String())
		if effort != "" {
			next, err := sjson.SetBytes(updated, "reasoning_effort", effort)
			if err != nil {
				return body, false, fmt.Errorf("set GLM reasoning_effort: %w", err)
			}
			updated = next
		}
		next, err := sjson.DeleteBytes(updated, "reasoning.effort")
		if err != nil {
			return body, false, fmt.Errorf("delete GLM reasoning.effort: %w", err)
		}
		updated = next
		changed = true
		if !gjson.GetBytes(updated, "reasoning").Exists() || len(gjson.GetBytes(updated, "reasoning").Map()) == 0 {
			next, err = sjson.DeleteBytes(updated, "reasoning")
			if err != nil {
				return body, false, fmt.Errorf("delete empty GLM reasoning: %w", err)
			}
			updated = next
		}
	}

	return updated, changed, nil
}
