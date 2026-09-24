package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSenseAudioAccountUsesDocumentedAPIEndpoints(t *testing.T) {
	account := &Account{
		Platform: PlatformSenseAudio,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
		},
	}
	require.NoError(t, ValidateAccountPlatform(PlatformSenseAudio))
	require.NoError(t, ValidateGroupPlatform(PlatformSenseAudio))
	require.NoError(t, validateAccountCredentials(PlatformSenseAudio, AccountTypeAPIKey, account.Credentials))
	require.Error(t, validateAccountCredentials(PlatformSenseAudio, AccountTypeOAuth, account.Credentials))
	require.True(t, account.IsCNProvider())
	require.True(t, account.IsOpenAICompatible())
	require.Equal(t, SenseAudioDefaultBaseURL, account.GetOpenAIBaseURL())
	require.Equal(t, "https://api.senseaudio.cn/v1/chat/completions", buildOpenAIEndpointURL(account.GetOpenAIBaseURL(), "/v1/chat/completions"))
	require.Equal(t, "test-key", account.GetOpenAIProtocolAPIKey())

	account.Credentials["api_protocol"] = APIProtocolResponses
	require.Equal(t, APIProtocolResponses, account.GetAPIProtocol())
	require.True(t, account.ShouldUseOpenAIResponsesAPI())
	require.Equal(t, "https://api.senseaudio.cn/v1/responses", buildOpenAIResponsesURLForPlatform(account.Platform, account.GetCNProtocolBaseURL(APIProtocolResponses)))

	account.Credentials["api_protocol"] = APIProtocolAnthropic
	require.Equal(t, SenseAudioDefaultBaseURL, account.GetAnthropicProtocolBaseURL())
	require.Equal(t, SenseAudioDefaultBaseURL, account.GetOpenAIFormatBaseURL())
	require.Equal(t, AnthropicAPIKeyAuthSchemeAuthorizationBearer, account.GetAnthropicAPIKeyAuthScheme())
	header := make(http.Header)
	setAnthropicAPIKeyAuthHeader(header, account, "test-key", account.GetAnthropicProtocolBaseURL())
	require.Equal(t, "Bearer test-key", header.Get("Authorization"))
	require.Empty(t, header.Get("x-api-key"))

	service := &OpenAIGatewayService{cfg: upstreamModelSyncTestConfig()}
	target, err := service.nativeAnthropicTargetURL(account)
	require.NoError(t, err)
	require.Equal(t, "https://api.senseaudio.cn/v1/messages", target)
}

func TestSenseAudioModelSyncOnlyRequestsLLMModels(t *testing.T) {
	account := &Account{
		Platform: PlatformSenseAudio,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
		},
	}
	service := &AccountTestService{cfg: upstreamModelSyncTestConfig()}
	req, err := service.buildUpstreamModelsRequest(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "https://api.senseaudio.cn/v1/models", req.URL.Scheme+"://"+req.URL.Host+req.URL.Path)
	require.Equal(t, "llm", req.URL.Query().Get("mode"))
	require.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
}
