package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestTierflowMessagesUseAccountCatalogWithoutOpenAIDefaults(t *testing.T) {
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformTierflow}}
	for _, model := range []string{"claude-opus-4-6", "claude-sonnet-4-5", "claude-haiku-4-5", "GLM-5.3", "TierSense"} {
		require.Empty(t, resolveOpenAIMessagesDispatchMappedModel(nil, apiKey, model))
	}
}

func TestTierflowResponsesUsePreOutputKeepalive(t *testing.T) {
	// Tierflow streams can remain silent during reasoning. The Responses-to-chat
	// bridge must enter the same keepalive path as the existing relay providers.
	require.True(t, isResponsesChatFallbackPlatform(service.PlatformTierflow))
	require.True(t, isResponsesChatFallbackPlatform(service.PlatformTokenRhythm))
	require.False(t, isResponsesChatFallbackPlatform(service.PlatformAnthropic))
}
