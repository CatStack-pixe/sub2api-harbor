package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestApplyGroupGlobalPromptDisabledIsNoOp(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	updated, changed, err := ApplyGroupGlobalPrompt(body, GlobalPromptProtocolOpenAIChat, &Group{GlobalPrompt: "ignore"})
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, updated)
}

func TestApplyGroupGlobalPromptProtocols(t *testing.T) {
	group := &Group{GlobalPromptEnabled: true, GlobalPrompt: "Follow the group rules."}
	tests := []struct {
		name     string
		protocol string
		body     string
		path     string
	}{
		{"anthropic", GlobalPromptProtocolAnthropic, `{"system":"client rules","messages":[{"role":"user","content":"hi"}]}`, "system.0.text"},
		{"chat", GlobalPromptProtocolOpenAIChat, `{"messages":[{"role":"user","content":"hi"}]}`, "messages.0.content"},
		{"responses", GlobalPromptProtocolResponses, `{"instructions":"client rules","input":"hi"}`, "instructions"},
		{"gemini", GlobalPromptProtocolGemini, `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`, "systemInstruction.parts.0.text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, changed, err := ApplyGroupGlobalPrompt([]byte(tt.body), tt.protocol, group)
			require.NoError(t, err)
			require.True(t, changed)
			require.Contains(t, gjson.GetBytes(updated, tt.path).String(), "Follow the group rules.")
		})
	}
}

func TestApplyGroupGlobalPromptPreservesExistingContent(t *testing.T) {
	group := &Group{GlobalPromptEnabled: true, GlobalPrompt: "server"}
	updated, changed, err := ApplyGroupGlobalPrompt([]byte(`{"messages":[{"role":"user","content":"hello"}]}`), GlobalPromptProtocolOpenAIChat, group)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "server", gjson.GetBytes(updated, "messages.0.content").String())
	require.Equal(t, "hello", gjson.GetBytes(updated, "messages.1.content").String())
}

func TestApplyGroupGlobalPromptPreservesProtocolSpecificInstructions(t *testing.T) {
	group := &Group{GlobalPromptEnabled: true, GlobalPrompt: "server"}
	tests := []struct {
		name            string
		protocol        string
		body            string
		serverPath      string
		clientPath      string
		clientValue     string
		additionalCheck func(t *testing.T, body []byte)
	}{
		{
			name:        "anthropic",
			protocol:    GlobalPromptProtocolAnthropic,
			body:        `{"system":"client","messages":[{"role":"user","content":"hello"}]}`,
			serverPath:  "system.0.text",
			clientPath:  "system.1.text",
			clientValue: "client",
		},
		{
			name:        "responses",
			protocol:    GlobalPromptProtocolResponses,
			body:        `{"instructions":"client","input":"hello"}`,
			serverPath:  "instructions",
			clientPath:  "instructions",
			clientValue: "server\n\nclient",
		},
		{
			name:        "gemini",
			protocol:    GlobalPromptProtocolGemini,
			body:        `{"systemInstruction":{"role":"system","parts":[{"text":"client"}]},"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`,
			serverPath:  "systemInstruction.parts.0.text",
			clientPath:  "systemInstruction.parts.1.text",
			clientValue: "client",
			additionalCheck: func(t *testing.T, body []byte) {
				require.Equal(t, "system", gjson.GetBytes(body, "systemInstruction.role").String())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, changed, err := ApplyGroupGlobalPrompt([]byte(tt.body), tt.protocol, group)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, "server", gjson.GetBytes(updated, tt.serverPath).String())
			require.Equal(t, tt.clientValue, gjson.GetBytes(updated, tt.clientPath).String())
			if tt.additionalCheck != nil {
				tt.additionalCheck(t, updated)
			}
		})
	}
}
