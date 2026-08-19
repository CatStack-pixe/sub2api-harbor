package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeAccountModelMappingCredentialsPreservesExistingTargets(t *testing.T) {
	credentials := map[string]any{
		"base_url": "https://example.com/v1",
		"model_mapping": map[string]any{
			"model-a": "custom-target",
		},
	}

	merged := mergeAccountModelMappingCredentials(credentials, map[string]string{
		"model-a": "model-a",
		"model-b": "model-b",
	})
	mapping, ok := merged["model_mapping"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "custom-target", mapping["model-a"])
	require.Equal(t, "model-b", mapping["model-b"])
	require.Equal(t, "https://example.com/v1", merged["base_url"])
}
