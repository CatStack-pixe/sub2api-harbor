package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexCompatibleCustomCatalogKeepsRetirementFallbackAndWildcardAliases(t *testing.T) {
	t.Parallel()

	for _, platform := range []string{PlatformChatAnywhere, PlatformSenseNova, PlatformDeepseek} {
		t.Run(platform, func(t *testing.T) {
			const groupID int64 = 118
			mapping := map[string]any{
				"public-*": "gpt-5.5", "gpt-5.5": "gpt-5.5",
				"hidden-legacy": "gpt-5.5", "codex-auto-review": "gpt-5.5",
			}
			accounts := []Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
					Credentials: map[string]any{"model_mapping": mapping}},
				{ID: 2, Platform: platform, Type: AccountTypeAPIKey,
					Credentials: map[string]any{"model_mapping": mapping}},
			}
			reasoning := true
			for i := range accounts {
				accounts[i].SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
					Models: map[string]UpstreamModelMetadata{"gpt-5.5": {
						ID: "gpt-5.5", Reasoning: &reasoning,
						DefaultReasoningLevel: "high", SupportedReasoningLevels: []string{"medium", "high"},
						InputModalities: []string{"text", "image"}, ContextWindow: 96_000,
					}},
				})
			}
			svc := &OpenAIGatewayService{accountRepo: codexModelsVisibilityAccountRepo{
				byGroup: map[int64][]Account{groupID: accounts},
			}}
			group := &Group{
				ID: groupID, Platform: PlatformOpenAI,
				ModelsListConfig: GroupModelsListConfig{
					Enabled: true, Models: []string{"public-legacy", "gpt-5.5", "codex-auto-review", "hidden-legacy"},
				},
				ModelAllowlist: GroupModelAllowlist{
					Enabled: true, Models: []string{"public-*", "gpt-5.5", "codex-auto-review"},
				},
			}
			manifest, configured, err := svc.BuildGroupConfiguredCodexModelsManifest(context.Background(), group, "")
			require.NoError(t, err)
			require.True(t, configured)
			require.NotNil(t, manifest)
			require.Equal(t, []string{"codex-auto-review", "gpt-5.5", "public-legacy"}, codexManifestModelSlugs(t, manifest.Body))
			for _, model := range decodeCodexManifestModels(t, manifest.Body) {
				if model["slug"] != "public-legacy" {
					continue
				}
				require.Equal(t, "public-legacy", model["display_name"])
				require.Equal(t, configuredCodexCustomDescription, model["description"])
				require.EqualValues(t, 96_000, model["context_window"])
				require.Equal(t, []string{"medium", "high"}, effortsFromManifestModel(t, model))
				wantModalities := []any{"text", "image"}
				if platform == PlatformChatAnywhere {
					wantModalities = []any{"text"}
				}
				require.Equal(t, wantModalities, model["input_modalities"])
			}
			require.NotContains(t, codexManifestModelSlugs(t, manifest.Body), "public-*")
			require.Equal(t, codexModelsManifestBodyETag(manifest.Body), manifest.ETag)
			cached, configured, err := svc.BuildGroupConfiguredCodexModelsManifest(context.Background(), group, manifest.ETag)
			require.NoError(t, err)
			require.True(t, configured)
			require.True(t, cached.NotModified)
			require.Empty(t, cached.Body)

			group.ModelAllowlist.Enabled = false
			customOnly, configured, err := svc.BuildGroupConfiguredCodexModelsManifest(context.Background(), group, "")
			require.NoError(t, err)
			require.True(t, configured)
			require.Contains(t, codexManifestModelSlugs(t, customOnly.Body), "public-legacy")
			require.Contains(t, codexManifestModelSlugs(t, customOnly.Body), "hidden-legacy")
		})
	}
}

func TestCodexCompatibleAliasConflictsDoNotGuessUpstreamCapabilities(t *testing.T) {
	t.Parallel()

	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Credentials: map[string]any{"model_mapping": map[string]any{"shared-coder": "gpt-5.6-sol"}}},
		{ID: 2, Platform: PlatformSenseNova, Type: AccountTypeAPIKey,
			Credentials: map[string]any{"model_mapping": map[string]any{"shared-coder": "gpt-6.1-sol"}}},
	}
	body, err := buildCodexModelsManifestForAccounts(
		PlatformOpenAI, []string{"shared-coder"}, accounts, nil, nil, true,
	)
	require.NoError(t, err)
	models := decodeCodexManifestModels(t, body)
	require.Len(t, models, 1)
	require.Equal(t, "shared-coder", models[0]["slug"])
	require.Equal(t, "shared-coder", models[0]["display_name"])
	require.Empty(t, effortsFromManifestModel(t, models[0]))
	require.Equal(t, []any{"text"}, models[0]["input_modalities"])
}

func TestCodexGeminiMixedCatalogExcludesAntigravityNonGeminiAliases(t *testing.T) {
	t.Parallel()

	native := Account{
		ID: 1, Platform: PlatformGemini, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{"claude-custom": "gemini-3.8-flash-high"}},
	}
	reasoning := true
	native.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
		Models: map[string]UpstreamModelMetadata{"gemini-3.8-flash-high": {
			ID: "gemini-3.8-flash-high", Reasoning: &reasoning,
			DefaultReasoningLevel: "high", SupportedReasoningLevels: []string{"high"},
			InputModalities: []string{"text", "image"}, ContextWindow: 1_048_576,
		}},
	})
	peer := Account{
		ID: 2, Platform: PlatformAntigravity, Type: AccountTypeOAuth,
		Extra:       map[string]any{"mixed_scheduling": true},
		Credentials: map[string]any{"model_mapping": map[string]any{"claude-custom": "claude-opus-4-8"}},
	}
	body, err := buildCodexModelsManifestForAccounts(
		PlatformGemini, []string{"claude-custom"}, []Account{native, peer}, nil, nil, true,
	)
	require.NoError(t, err)
	models := decodeCodexManifestModels(t, body)
	require.Len(t, models, 1)
	require.Equal(t, "claude-custom", models[0]["slug"])
	require.Equal(t, []string{"high"}, effortsFromManifestModel(t, models[0]))
	require.EqualValues(t, 1_048_576, models[0]["context_window"])
	require.Equal(t, []any{"text", "image"}, models[0]["input_modalities"])
}
