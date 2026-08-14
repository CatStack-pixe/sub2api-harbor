package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	GlobalPromptProtocolAnthropic = "anthropic"
	GlobalPromptProtocolOpenAIChat = "openai_chat"
	GlobalPromptProtocolResponses = "responses"
	GlobalPromptProtocolGemini = "gemini"
)

// ApplyGroupGlobalPrompt prepends a group's static instruction to a supported
// text-generation request. Disabled or empty prompts are a strict no-op.
func ApplyGroupGlobalPrompt(body []byte, protocol string, group *Group) ([]byte, bool, error) {
	if group == nil || !group.GlobalPromptEnabled || strings.TrimSpace(group.GlobalPrompt) == "" {
		return body, false, nil
	}
	prompt := strings.TrimSpace(group.GlobalPrompt)
	if err := ValidateGroupGlobalPrompt(prompt); err != nil {
		return nil, false, err
	}

	var request map[string]json.RawMessage
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, false, fmt.Errorf("parse request for group global prompt: %w", err)
	}
	if request == nil {
		return nil, false, fmt.Errorf("request must be a JSON object")
	}

	var err error
	switch protocol {
	case GlobalPromptProtocolAnthropic:
		err = prependAnthropicSystem(request, prompt)
	case GlobalPromptProtocolOpenAIChat:
		err = prependOpenAIChatSystem(request, prompt)
	case GlobalPromptProtocolResponses:
		err = prependResponsesInstructions(request, prompt)
	case GlobalPromptProtocolGemini:
		err = prependGeminiSystemInstruction(request, prompt)
	default:
		return body, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	updated, err := json.Marshal(request)
	if err != nil {
		return nil, false, fmt.Errorf("marshal request with group global prompt: %w", err)
	}
	return updated, true, nil
}

func prependAnthropicSystem(request map[string]json.RawMessage, prompt string) error {
	blocks := []any{map[string]any{"type": "text", "text": prompt}}
	if raw, ok := request["system"]; ok && len(raw) > 0 && string(raw) != "null" {
		var existing any
		if err := json.Unmarshal(raw, &existing); err != nil {
			return fmt.Errorf("parse anthropic system: %w", err)
		}
		switch value := existing.(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": value})
			}
		case []any:
			blocks = append(blocks, value...)
		default:
			return fmt.Errorf("anthropic system must be a string or array")
		}
	}
	encoded, err := json.Marshal(blocks)
	if err != nil {
		return err
	}
	request["system"] = encoded
	return nil
}

func prependOpenAIChatSystem(request map[string]json.RawMessage, prompt string) error {
	var messages []any
	if raw, ok := request["messages"]; ok && len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &messages); err != nil {
			return fmt.Errorf("parse openai messages: %w", err)
		}
	}
	messages = append([]any{map[string]any{"role": "system", "content": prompt}}, messages...)
	encoded, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	request["messages"] = encoded
	return nil
}

func prependResponsesInstructions(request map[string]json.RawMessage, prompt string) error {
	instructions := ""
	if raw, ok := request["instructions"]; ok && len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &instructions); err != nil {
			return fmt.Errorf("parse responses instructions: %w", err)
		}
	}
	if strings.TrimSpace(instructions) == "" {
		instructions = prompt
	} else {
		instructions = prompt + "\n\n" + instructions
	}
	encoded, err := json.Marshal(instructions)
	if err != nil {
		return err
	}
	request["instructions"] = encoded
	return nil
}

func prependGeminiSystemInstruction(request map[string]json.RawMessage, prompt string) error {
	parts := []any{map[string]any{"text": prompt}}
	var role json.RawMessage
	if raw, ok := request["systemInstruction"]; ok && len(raw) > 0 && string(raw) != "null" {
		var existing map[string]json.RawMessage
		if err := json.Unmarshal(raw, &existing); err != nil {
			return fmt.Errorf("parse gemini systemInstruction: %w", err)
		}
		if rawParts := existing["parts"]; len(rawParts) > 0 && string(rawParts) != "null" {
			var existingParts []any
			if err := json.Unmarshal(rawParts, &existingParts); err != nil {
				return fmt.Errorf("parse gemini systemInstruction.parts: %w", err)
			}
			parts = append(parts, existingParts...)
		}
		role = existing["role"]
	}
	updated := map[string]any{"parts": parts}
	if len(role) > 0 && string(role) != "null" {
		updated["role"] = role
	}
	encoded, err := json.Marshal(updated)
	if err != nil {
		return err
	}
	request["systemInstruction"] = encoded
	return nil
}
