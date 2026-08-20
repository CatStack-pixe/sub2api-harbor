//go:build unit

package service

import "testing"

func TestChatAnywhereAccountCredentialsAndBaseURLs(t *testing.T) {
	for _, baseURL := range []string{"", ChatAnywhereChinaBaseURL, ChatAnywhereGlobalBaseURL, ChatAnywhereChinaBaseURL + "/"} {
		credentials := map[string]any{"api_key": "chatanywhere-key"}
		if baseURL != "" {
			credentials["base_url"] = baseURL
		}
		if err := validateAccountCredentials(PlatformChatAnywhere, AccountTypeAPIKey, credentials); err != nil {
			t.Fatalf("valid credentials rejected for %q: %v", baseURL, err)
		}
	}

	for _, credentials := range []map[string]any{
		{},
		{"api_key": "chatanywhere-key", "base_url": "https://relay.example/v1"},
		{"api_key": "chatanywhere-key", "base_url": 42},
	} {
		if err := validateAccountCredentials(PlatformChatAnywhere, AccountTypeAPIKey, credentials); err == nil {
			t.Fatalf("invalid credentials accepted: %#v", credentials)
		}
	}
	if err := validateAccountCredentials(PlatformChatAnywhere, AccountTypeOAuth, map[string]any{"api_key": "chatanywhere-key"}); err == nil {
		t.Fatal("OAuth credentials unexpectedly accepted")
	}

	defaultAccount := &Account{Platform: PlatformChatAnywhere, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "key"}}
	globalAccount := &Account{Platform: PlatformChatAnywhere, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "key", "base_url": ChatAnywhereGlobalBaseURL}}
	customAccount := &Account{Platform: PlatformChatAnywhere, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "key", "base_url": "https://relay.example/v1"}}
	if got := defaultAccount.GetBaseURL(); got != ChatAnywhereChinaBaseURL {
		t.Fatalf("default Anthropic base URL = %q", got)
	}
	if got := globalAccount.GetBaseURL(); got != ChatAnywhereGlobalBaseURL {
		t.Fatalf("global Anthropic base URL = %q", got)
	}
	if got := defaultAccount.GetOpenAIBaseURL(); got != ChatAnywhereChinaBaseURL {
		t.Fatalf("default base URL = %q", got)
	}
	if got := globalAccount.GetOpenAIBaseURL(); got != ChatAnywhereGlobalBaseURL {
		t.Fatalf("global base URL = %q", got)
	}
	if got := customAccount.GetOpenAIBaseURL(); got != ChatAnywhereChinaBaseURL {
		t.Fatalf("custom base URL = %q", got)
	}
	if !defaultAccount.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions) {
		t.Fatal("ChatAnywhere should support Chat Completions")
	}
	if defaultAccount.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses) ||
		defaultAccount.ShouldUseOpenAIResponsesAPI() {
		t.Fatal("ChatAnywhere must bridge Responses through Chat Completions")
	}
}
