package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestResolveGroupRequestModelUsesAgnesGroupConfig(t *testing.T) {
	apiKey := &service.APIKey{Group: &service.Group{
		Platform: service.PlatformAgnes,
		ModelsListConfig: service.GroupModelsListConfig{
			ModelMappingEnabled: true,
			ModelMapping: map[string]string{
				"deepseek-v4-pro": "agnes-2.5-pro-alpha",
			},
		},
	}}

	mapped, matched := resolveGroupRequestModel(apiKey, "deepseek-v4-pro")
	require.True(t, matched)
	require.Equal(t, "agnes-2.5-pro-alpha", mapped)
}

func TestEffectiveOpenAIForwardModelPrefersChannelMapping(t *testing.T) {
	mapped, changed := effectiveOpenAIForwardModel("agnes-2.5-pro-alpha", true, service.ChannelMappingResult{
		Mapped:      true,
		MappedModel: "agnes-2.5-flash",
	})

	require.True(t, changed)
	require.Equal(t, "agnes-2.5-flash", mapped)
}
