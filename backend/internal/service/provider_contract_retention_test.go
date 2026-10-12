//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSenseNovaAdaptiveConfiguredAnthropicEndpointIsUsedAndProbed(t *testing.T) {
	account := adaptiveCNAccountTestAccount(909, PlatformSenseNova)
	require.False(t, LookupProviderProfile(PlatformSenseNova).SupportsProtocol("", APIProtocolAnthropic))
	require.Equal(t, APIProtocolAnthropic, resolveUpstreamProtocol(account, APIProtocolAnthropic, "", nil))
	require.Equal(t, "http://anthropic.example", account.GetCNProtocolBaseURL(APIProtocolAnthropic))
	svc, upstream := adaptiveCNAccountTestService(account, adaptiveCNChatTestResponse(), adaptiveCNAnthropicTestResponse())
	c, _ := newTestContext()
	require.NoError(t, svc.TestAccountConnection(c, account.ID, "SenseChat", "", AccountTestModeDefault))
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "http://anthropic.example/v1/messages", upstream.requests[1].URL.String())

	delete(account.Credentials["api_base_urls"].(map[string]any), APIProtocolAnthropic)
	require.Equal(t, APIProtocolChatCompletions, resolveUpstreamProtocol(account, APIProtocolAnthropic, "", nil))
}

func TestClineWalletLimitsDoNotAdvertiseAccountWideThresholds(t *testing.T) {
	_, err := validateAndNormalizeAccountSchedulingThresholds(map[string]int{PlatformCline: 80})
	require.Error(t, err)
	normalized, err := validateAndNormalizeAccountSchedulingThresholds(map[string]int{PlatformCommandCode: 80})
	require.NoError(t, err)
	require.Equal(t, 80, normalized[PlatformCommandCode])
}
