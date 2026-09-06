package service

import (
	"encoding/json"
	"testing"
)

func TestSanitizeUnsupportedCNImageInput(t *testing.T) {
	tests := []struct {
		name       string
		platform   string
		model      string
		body       string
		wantChange bool
		wantType   string
	}{
		{
			name:       "chat image url",
			platform:   PlatformDeepseek,
			model:      "deepseek-v4-flash-0731",
			body:       `{"model":"deepseek-v4-flash-0731","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`,
			wantChange: true,
			wantType:   "text",
		},
		{
			name:       "responses input image",
			platform:   PlatformDeepseek,
			model:      "deepseek-v4-pro-0813",
			body:       `{"model":"deepseek-v4-pro-0813","input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]}]}`,
			wantChange: true,
			wantType:   "input_text",
		},
		{
			name:       "zhipu historical message",
			platform:   PlatformZhipu,
			model:      "glm-5.3",
			body:       `{"model":"glm-5.3","messages":[{"role":"user","content":"first"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/image.png"}}]}]}`,
			wantChange: true,
			wantType:   "text",
		},
		{
			name:       "deepseek vision model is allowed",
			platform:   PlatformDeepseek,
			model:      "deepseek-v4-flash-vision-exp",
			body:       `{"model":"deepseek-v4-flash-vision-exp","input":[{"type":"input_image","image_url":"https://example.invalid/image.png"}]}`,
			wantChange: false,
		},
		{
			name:       "non cn platform is unchanged",
			platform:   PlatformOpenAI,
			model:      "gpt-5",
			body:       `{"model":"gpt-5","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/image.png"}}]}]}`,
			wantChange: false,
		},
		{
			name:       "text only request is unchanged",
			platform:   PlatformZhipu,
			model:      "glm-5.3",
			body:       `{"model":"glm-5.3","messages":[{"role":"user","content":"hello"}]}`,
			wantChange: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed, err := SanitizeUnsupportedCNImageInput([]byte(tt.body), tt.platform, tt.model)
			if err != nil {
				t.Fatalf("SanitizeUnsupportedCNImageInput() error = %v", err)
			}
			if changed != tt.wantChange {
				t.Fatalf("changed = %v, want %v", changed, tt.wantChange)
			}
			if !tt.wantChange {
				if string(got) != tt.body {
					t.Fatalf("unchanged body = %s, want %s", got, tt.body)
				}
				return
			}

			var request map[string]any
			if err := json.Unmarshal(got, &request); err != nil {
				t.Fatalf("sanitized body is invalid JSON: %v", err)
			}
			if hasOpenAIInputImage(request) {
				t.Fatal("sanitized request still contains image input")
			}
			if tt.wantType == "input_text" {
				content := request["input"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
				if content["type"] != tt.wantType {
					t.Fatalf("replacement type = %v, want %s", content["type"], tt.wantType)
				}
			}
		})
	}
}

func TestSanitizeUnsupportedCNImageInputRejectsMalformedJSON(t *testing.T) {
	_, changed, err := SanitizeUnsupportedCNImageInput([]byte(`{"input":[`), PlatformDeepseek, "deepseek-v4-flash")
	if err == nil {
		t.Fatal("expected malformed JSON error")
	}
	if changed {
		t.Fatal("malformed request must not be marked changed")
	}
}
