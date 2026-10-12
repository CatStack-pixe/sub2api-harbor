//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewProviderCredentialsRejectInvalidAccountInputs(t *testing.T) {
	for _, platform := range []string{PlatformCommandCode, PlatformCline} {
		t.Run(platform, func(t *testing.T) {
			require.NoError(t, ValidateAccountPlatform(platform))
			require.NoError(t, ValidateGroupPlatform(platform))
			require.NoError(t, validateAccountCredentials(platform, AccountTypeAPIKey, map[string]any{"api_key": "test-key"}))
			require.NoError(t, validateAccountCredentials(platform, AccountTypeAPIKey, map[string]any{
				"api_key": "test-key", "base_url": "https://relay.example/v1",
			}))
			require.Error(t, validateAccountCredentials(platform, AccountTypeOAuth, map[string]any{"api_key": "test-key"}))
			require.Error(t, validateAccountCredentials(platform, AccountTypeAPIKey, nil))
			require.Error(t, validateAccountCredentials(platform, AccountTypeAPIKey, map[string]any{"api_key": "  "}))
			require.Error(t, validateAccountCredentials(platform, AccountTypeAPIKey, map[string]any{
				"api_key": "test-key", "base_url": "file:///tmp/provider",
			}))
			require.Error(t, validateAccountCredentials(platform, AccountTypeAPIKey, map[string]any{
				"api_key": "test-key", "base_url": 42,
			}))
		})
	}
	require.Error(t, ValidateAccountPlatform(PlatformComposite))
	require.Error(t, ValidateAccountPlatform("unregistered-provider"))
	require.Error(t, ValidateGroupPlatform("unregistered-provider"))
}
