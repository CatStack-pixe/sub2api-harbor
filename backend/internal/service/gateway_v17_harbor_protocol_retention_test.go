//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Selecting a protocol-specific endpoint must retain Harbor providers'
// authentication and path conventions.
func TestV17AnthropicEndpointSelectionRetainsHarborProviderAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform string
		baseURL  string
		wantURL  string
	}{
		{
			name: "ChatAnywhere",
			platform: PlatformChatAnywhere,
			baseURL: ChatAnywhereGlobalBaseURL,
			wantURL: ChatAnywhereGlobalBaseURL + "/messages",
		},
		{
			name: "TokenRhythm",
			platform: PlatformTokenRhythm,
			baseURL: "https://tokenrhythm.example",
			wantURL: "https://tokenrhythm.example/v1/messages?beta=true",
		},
		{
			name: "SenseAudio",
			platform: PlatformSenseAudio,
			baseURL: "https://senseaudio.example",
			wantURL: "https://senseaudio.example/v1/messages?beta=true",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{
				Platform: tc.platform,
				Type: AccountTypeAPIKey,
				Credentials: map[string]any{"base_url": tc.baseURL},
			}
			svc := &GatewayService{cfg: &config.Config{}}
			body := []byte(`{"model":"claude-sonnet-5","max_tokens":128,"messages":[{"role":"user","content":"hi"}]}`)
			req, _, err := svc.buildUpstreamRequest(
				context.Background(), gatewayAnthropicBaseURLContext(t), account,
				body, "fixture-key", "apikey", "claude-sonnet-5", false, false,
			)
			require.NoError(t, err)
			require.Equal(t, tc.wantURL, req.URL.String())
			require.Equal(t, "Bearer fixture-key", req.Header.Get("Authorization"))
			require.Empty(t, req.Header.Get("X-Api-Key"))
		})
	}
}
