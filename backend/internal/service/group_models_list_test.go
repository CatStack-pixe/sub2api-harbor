package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeGroupModelsListConfigPreservesCleanAgnesMapping(t *testing.T) {
	modelMapping := make(map[string]string)
	modelMapping[" deepseek-v4-pro "] = " agnes-2.5-pro-alpha "
	modelMapping["deepseek-v4-flash"] = "agnes-2.5-flash"
	modelMapping["missing-upstream"] = " "
	modelMapping[" "] = "agnes-2.0-flash"

	cfg := normalizeGroupModelsListConfig(GroupModelsListConfig{
		Enabled:             true,
		Models:              []string{" deepseek-v4-pro ", "deepseek-v4-pro", ""},
		ModelMappingEnabled: true,
		ModelMapping:        modelMapping,
	})

	require.Equal(t, []string{"deepseek-v4-pro"}, cfg.Models)
	require.Equal(t, map[string]string{"deepseek-v4-pro": "agnes-2.5-pro-alpha", "deepseek-v4-flash": "agnes-2.5-flash"}, cfg.ModelMapping)
}

func TestGroupResolveRequestModelOnlyAppliesToEnabledAgnesGroups(t *testing.T) {
	group := &Group{
		Platform: PlatformAgnes,
		ModelsListConfig: GroupModelsListConfig{
			ModelMappingEnabled: true,
			ModelMapping:        map[string]string{"deepseek-v4-pro": "agnes-2.5-pro-alpha", "deepseek-v4-*": "agnes-2.5-flash"},
		},
	}

	mapped, matched := group.ResolveRequestModel("deepseek-v4-pro")
	require.True(t, matched)
	require.Equal(t, "agnes-2.5-pro-alpha", mapped)

	mapped, matched = group.ResolveRequestModel("deepseek-v4-fast")
	require.True(t, matched)
	require.Equal(t, "agnes-2.5-flash", mapped)

	group.ModelsListConfig.ModelMappingEnabled = false
	mapped, matched = group.ResolveRequestModel("deepseek-v4-pro")
	require.False(t, matched)
	require.Equal(t, "deepseek-v4-pro", mapped)

	group.ModelsListConfig.ModelMappingEnabled = true
	group.Platform = PlatformOpenAI
	_, matched = group.ResolveRequestModel("deepseek-v4-pro")
	require.False(t, matched)
}
