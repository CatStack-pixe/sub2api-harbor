package service

import (
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// normalizeCNChatCompletionsStopField adapts the OpenAI-compatible stop field
// for CN providers whose schema requires an array of strings (for example,
// Zhipu's Chat Completions endpoint). OpenAI clients may legally send a single
// string, so convert that form without changing an already valid array.
func normalizeCNChatCompletionsStopField(body []byte) ([]byte, bool, error) {
	stop := gjson.GetBytes(body, "stop")
	if !stop.Exists() || stop.IsArray() {
		return body, false, nil
	}

	switch stop.Type {
	case gjson.Null:
		updated, err := sjson.DeleteBytes(body, "stop")
		if err != nil {
			return body, false, fmt.Errorf("remove null stop field: %w", err)
		}
		return updated, true, nil
	case gjson.String:
		value := stop.String()
		stops := []string{value}
		if value == "" {
			stops = []string{}
		}
		updated, err := sjson.SetBytes(body, "stop", stops)
		if err != nil {
			return body, false, fmt.Errorf("normalize stop field: %w", err)
		}
		return updated, true, nil
	default:
		// Preserve unsupported values so the provider can return its normal
		// validation error rather than silently changing client intent.
		return body, false, nil
	}
}

func shouldNormalizeCNChatCompletionsStop(account *Account) bool {
	if account == nil {
		return false
	}
	return account.Platform == PlatformZhipu || account.Platform == PlatformGLM
}
