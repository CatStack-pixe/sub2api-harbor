package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIModelRetirementKeepsSupportedCompatibleProviders(t *testing.T) {
	t.Parallel()

	for _, platform := range []string{
		PlatformAgnes, PlatformNvidia, PlatformChatAnywhere,
		PlatformSenseNova, PlatformDeepseek, PlatformGrok,
	} {
		t.Run(platform, func(t *testing.T) {
			mapping := map[string]any{"gpt-5.5": "gpt-5.5", "legacy-*": "gpt-5.5"}
			oauth := Account{
				ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{"model_mapping": mapping},
			}
			provider := Account{
				ID: 2, Platform: platform, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"model_mapping": mapping},
			}
			models := []string{"gpt-5.5", "legacy-coder"}
			for _, accounts := range [][]Account{{oauth, provider}, {provider, oauth}} {
				for _, model := range models {
					require.False(t, oauth.IsModelSupported(model))
					require.True(t, provider.IsModelSupported(model))
					require.False(t, openAIModelRetiredForAccounts(accounts, model))
				}
				require.Equal(t, models, filterRetiredOpenAIModelIDs(accounts, models))
				body, err := buildCodexModelsManifestForAccounts(
					PlatformOpenAI, models, accounts, nil, nil, true,
				)
				require.NoError(t, err)
				require.Equal(t, models, codexManifestModelSlugs(t, body))
			}
		})
	}
}

func TestOpenAIModelRetirementRequiresAnEligibleCompatibleProvider(t *testing.T) {
	t.Parallel()

	oauth := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	parentID := int64(1)
	for _, tc := range []struct {
		name     string
		provider Account
		retired  bool
	}{
		{
			name:     "unmapped compatible relay",
			provider: Account{Platform: PlatformChatAnywhere, Type: AccountTypeAPIKey},
		},
		{
			name: "compatible administrator replacement",
			provider: Account{Platform: PlatformDeepseek, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.5": "deepseek-v4-pro"}}},
		},
		{
			name: "compatible mapping excludes requested model",
			provider: Account{Platform: PlatformChatAnywhere, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"}}},
			retired: true,
		},
		{
			name:     "unmapped DeepSeek does not serve GPT",
			provider: Account{Platform: PlatformDeepseek, Type: AccountTypeAPIKey},
			retired:  true,
		},
		{
			name:     "unrelated API key platform",
			provider: Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
			retired:  true,
		},
		{
			name:     "unrelated Gemini API key",
			provider: Account{Platform: PlatformGemini, Type: AccountTypeAPIKey},
			retired:  true,
		},
		{
			name: "Antigravity mixed scheduling is not an OpenAI route",
			provider: Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth,
				Extra: map[string]any{"mixed_scheduling": true}},
			retired: true,
		},
		{
			name:     "shadow API key still uses subscription",
			provider: Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, ParentAccountID: &parentID},
			retired:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, accounts := range [][]Account{{oauth, tc.provider}, {tc.provider, oauth}} {
				require.Equal(t, tc.retired, openAIModelRetiredForAccounts(accounts, "gpt-5.5"))
				require.False(t, openAIModelRetiredForAccounts(accounts, "unrecognized-model"))
			}
		})
	}
}

func TestOpenAIModelRetirementMixedProviderPublicCatalogAndETag(t *testing.T) {
	t.Parallel()

	const groupID int64 = 117
	oauth := Account{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"gpt-5.5": "gpt-5.5", "legacy-coder": "gpt-5.5",
		}},
	}
	for _, supported := range []bool{false, true} {
		name := "provider excludes retired names"
		mapping := map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"}
		if supported {
			name = "provider serves retired names"
			mapping = map[string]any{"gpt-5.5": "gpt-5.5", "legacy-*": "gpt-5.5"}
		}
		t.Run(name, func(t *testing.T) {
			accounts := []Account{oauth, {
				ID: 2, Platform: PlatformChatAnywhere, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"model_mapping": mapping},
			}}
			for _, codex := range []bool{false, true} {
				field, idField := "data", "id"
				if codex {
					field, idField = "models", "slug"
				}
				body := []byte(`{"` + field + `":[{"` + idField + `":"gpt-5.5","extension":1},{"` +
					idField + `":"legacy-coder","extension":2},{"` + idField +
					`":"gpt-6.1-sol","extension":3}],"unknown_top":true}`)
				original := string(body)
				filtered, changed, err := filterRetiredOpenAIPublicModelsBody(body, accounts, codex)
				require.NoError(t, err)
				require.Equal(t, original, string(body), "shared upstream body must remain intact")
				require.Equal(t, !supported, changed)
				require.True(t, gjson.GetBytes(filtered, "unknown_top").Bool())
				if supported {
					require.Equal(t, body, filtered)
				} else {
					entries := gjson.GetBytes(filtered, field).Array()
					require.Len(t, entries, 1)
					require.Equal(t, "gpt-6.1-sol", entries[0].Get(idField).String())
					require.Equal(t, int64(3), entries[0].Get("extension").Int())
				}
				if !codex {
					continue
				}
				group := &Group{ID: groupID, Platform: PlatformOpenAI}
				group.CodexModelsManifestConfig.Enabled = true
				svc := &OpenAIGatewayService{accountRepo: codexModelsVisibilityAccountRepo{
					byGroup: map[int64][]Account{groupID: accounts},
				}}
				upstreamETag := CodexModelsManifestETag(body)
				manifest := &OpenAIModelsResponse{Body: body, ETag: upstreamETag}
				require.NoError(t, svc.MergeGroupConfiguredCodexModels(context.Background(), group, manifest, ""))
				require.JSONEq(t, string(filtered), string(manifest.Body))
				require.Equal(t, codexModelsManifestBodyETag(manifest.Body), manifest.ETag)

				conditional := &OpenAIModelsResponse{Body: body, ETag: upstreamETag}
				require.NoError(t, svc.MergeGroupConfiguredCodexModels(context.Background(), group, conditional, upstreamETag))
				require.Equal(t, supported, conditional.NotModified)
				require.Equal(t, manifest.ETag, conditional.ETag)
				if !supported {
					require.JSONEq(t, string(filtered), string(conditional.Body))
				}
				cached := &OpenAIModelsResponse{Body: body, ETag: upstreamETag}
				require.NoError(t, svc.MergeGroupConfiguredCodexModels(context.Background(), group, cached, manifest.ETag))
				require.True(t, cached.NotModified)
				require.Empty(t, cached.Body)
			}
		})
	}
}
