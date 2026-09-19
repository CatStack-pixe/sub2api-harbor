package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotGroupForceOpenAIFastRoundtrip(t *testing.T) {
	groupID := int64(50)
	apiKey := &APIKey{
		ID: 82, UserID: 40, GroupID: &groupID, Key: "sk-fast-roundtrip", Status: StatusActive,
		User: &User{ID: 40, Status: StatusActive},
		Group: &Group{
			ID: groupID, Name: "fast-roundtrip", Platform: PlatformOpenAI, Status: StatusActive,
			Hydrated: true, ForceOpenAIFast: true, FreeOpenAIFast: true,
		},
	}
	svc := &APIKeyService{}

	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), apiKey)})
	require.NoError(t, err)
	var cached APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &cached))

	materialized, used, err := svc.applyAuthCacheEntry(apiKey.Key, &cached)
	require.NoError(t, err)
	require.True(t, used)
	require.NotNil(t, materialized.Group)
	require.True(t, materialized.Group.Hydrated)
	require.True(t, materialized.Group.ForceOpenAIFast)
	require.True(t, materialized.Group.FreeOpenAIFast)
	require.Equal(t, apiKeyAuthSnapshotVersion, cached.Snapshot.Version)
}

func TestAPIKeyAuthSnapshotPreservesIndependentModelPolicies(t *testing.T) {
	svc := &APIKeyService{}
	apiKey := profitAuthTestAPIKey()
	apiKey.Group.Platform = PlatformAgnes
	apiKey.Group.ModelsListConfig = GroupModelsListConfig{
		Enabled: true,
		Models: []string{"public-alias"},
		ModelMappingEnabled: true,
		ModelMapping: map[string]string{"public-alias": "agnes-2.5-pro-alpha"},
	}
	apiKey.Group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"other-allowed-model"}}
	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), apiKey)})
	require.NoError(t, err)
	var cached APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &cached))
	materialized, used, err := svc.applyAuthCacheEntry(apiKey.Key, &cached)
	require.NoError(t, err)
	require.True(t, used)
	require.Equal(t, apiKey.Group.ModelsListConfig, materialized.Group.ModelsListConfig)
	require.Equal(t, apiKey.Group.ModelAllowlist, materialized.Group.ModelAllowlist)
	mapped, ok := materialized.Group.ResolveRequestModel("public-alias")
	require.True(t, ok)
	require.Equal(t, "agnes-2.5-pro-alpha", mapped)

	// A snapshot created before the fork listing fields were restored must reload.
	cached.Snapshot.Version = 24
	materialized, used, err = svc.applyAuthCacheEntry(apiKey.Key, &cached)
	require.NoError(t, err)
	require.False(t, used)
	require.Nil(t, materialized)
}
