package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newCodexProtocolTestContext(headers map[string]string) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	for name, value := range headers {
		context.Request.Header.Set(name, value)
	}
	return context
}

func TestCodexSessionHeadersDriveSchedulingAndWebSocketResolution(t *testing.T) {
	context := newCodexProtocolTestContext(map[string]string{
		codexSessionIDHeader: "codex-session",
		codexThreadIDHeader:  "codex-thread",
	})

	require.Equal(t, "codex-session", explicitOpenAIHeaderSessionID(context))
	resolution := resolveOpenAIWSSessionHeaders(context, "")
	require.Equal(t, "codex-session", resolution.SessionID)
	require.Equal(t, "codex-thread", resolution.ConversationID)
	require.Equal(t, "codex-session", resolveOpenAICompactSessionID(context))
}

func TestCodexInstallationIDIsOwnedByOAuthAccount(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"openai_device_id": "account-installation"},
	}
	headers := make(http.Header)
	headers.Set("x-codex-installation-id", "caller-installation")

	require.True(t, applyCodexInstallationIDHeader(headers, account))
	require.Equal(t, "account-installation", headers.Get("x-codex-installation-id"))

	headers.Set("x-codex-installation-id", "caller-installation")
	require.False(t, applyCodexInstallationIDHeader(headers, &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}))
	require.Empty(t, headers.Get("x-codex-installation-id"))

	body := map[string]any{
		"client_metadata": map[string]any{
			"x-codex-installation-id": "caller-installation",
			"x-codex-turn-metadata":   "turn-metadata",
		},
	}
	require.True(t, applyCodexClientMetadata(body, account))
	metadata, ok := body["client_metadata"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "account-installation", metadata["x-codex-installation-id"])
	require.Equal(t, "turn-metadata", metadata["x-codex-turn-metadata"])
}

func TestOpenAIAllowedHeadersIncludeCodexSessionHeaders(t *testing.T) {
	require.True(t, openaiAllowedHeaders[codexSessionIDHeader])
	require.True(t, openaiAllowedHeaders[codexThreadIDHeader])
	require.True(t, openaiPassthroughAllowedHeaders[codexSessionIDHeader])
	require.True(t, openaiPassthroughAllowedHeaders[codexThreadIDHeader])
}
