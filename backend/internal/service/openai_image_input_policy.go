package service

import (
	"encoding/json"
	"strings"
)

const unsupportedCNImageInputPlaceholder = "[Image input omitted: selected model does not support vision.]"

// SanitizeUnsupportedCNImageInput prevents text-only Chinese-provider models
// from receiving image parts that their upstream rejects with a 400. The
// original request is returned untouched when the platform/model is allowed
// to receive images or when no image part is present.
func SanitizeUnsupportedCNImageInput(body []byte, platform, model string) ([]byte, bool, error) {
	if !unsupportedCNImageInputPolicyApplies(platform, model) {
		return body, false, nil
	}

	var request map[string]any
	if err := decodeOpenAIJSONUseNumber(body, &request); err != nil {
		return body, false, err
	}
	if !hasOpenAIInputImage(request) {
		return body, false, nil
	}

	changed := false
	for _, key := range []string{"input", "messages"} {
		value, ok := request[key]
		if !ok {
			continue
		}
		sanitized, valueChanged := sanitizeUnsupportedCNImageInputValue(value)
		if valueChanged {
			request[key] = sanitized
			changed = true
		}
	}
	if !changed {
		return body, false, nil
	}

	rebuilt, err := json.Marshal(request)
	if err != nil {
		return body, false, err
	}
	return rebuilt, true, nil
}

func unsupportedCNImageInputPolicyApplies(platform, model string) bool {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform != PlatformDeepseek && platform != PlatformZhipu {
		return false
	}
	return !cnModelSupportsImageInput(model)
}

func cnModelSupportsImageInput(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	if model == "deepseek-v4-flash-vision-exp" {
		return true
	}
	if model == "glm-5.3-flash" {
		return true
	}
	for _, marker := range []string{"vision", "-vl", "glm-4v", "glm-4.5v", "glm-4.6v", "glm-5v"} {
		if strings.Contains(model, marker) {
			return true
		}
	}
	return strings.HasSuffix(model, "-v")
}

func sanitizeUnsupportedCNImageInputValue(value any) (any, bool) {
	switch value := value.(type) {
	case []any:
		changed := false
		for i, item := range value {
			sanitized, itemChanged := sanitizeUnsupportedCNImageInputValue(item)
			if itemChanged {
				value[i] = sanitized
				changed = true
			}
		}
		return value, changed
	case map[string]any:
		typ := strings.ToLower(strings.TrimSpace(firstNonEmptyString(value["type"])))
		switch typ {
		case "image_url":
			return map[string]any{"type": "text", "text": unsupportedCNImageInputPlaceholder}, true
		case "input_image":
			return map[string]any{"type": "input_text", "text": unsupportedCNImageInputPlaceholder}, true
		}
		if _, ok := value["image_url"]; ok {
			return map[string]any{"type": "text", "text": unsupportedCNImageInputPlaceholder}, true
		}
		changed := false
		for key, item := range value {
			sanitized, itemChanged := sanitizeUnsupportedCNImageInputValue(item)
			if itemChanged {
				value[key] = sanitized
				changed = true
			}
		}
		return value, changed
	default:
		return value, false
	}
}
