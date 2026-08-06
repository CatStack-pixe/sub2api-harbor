package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
)

func TestFilterGroupModelOptions(t *testing.T) {
	models := []string{"gpt-5.6", "gpt-5.5", "claude-sonnet-4"}

	require.Equal(t, []string{"gpt-5.6", "gpt-5.5"}, filterGroupModelOptions(models, []string{"gpt-5.*"}))
	require.Equal(t, []string{"claude-sonnet-4"}, filterGroupModelOptions(models, []string{"claude-sonnet-4"}))
	require.Equal(t, models, filterGroupModelOptions(models, nil))
}

func TestGroupRequestModelOptionsSortsAliases(t *testing.T) {
	group := &Group{ModelsListConfig: GroupModelsListConfig{ModelMapping: map[string]string{
		"z-model": "upstream-z",
		"a-model": "upstream-a",
	}}}

	require.Equal(t, []string{"a-model", "z-model"}, groupRequestModelOptions(group))
}

func TestGroupModelOptionsPreferAccountMappings(t *testing.T) {
	group := &Group{Platform: PlatformOpenAI}
	accounts := []Account{{
		Platform: PlatformOpenAI,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"custom-gpt": "upstream-gpt",
			},
		},
	}}

	require.Equal(t, []string{"custom-gpt"}, groupModelOptionsFromAccounts(group, accounts))
}

func TestGroupModelOptionsAnthropicDefaultsIncludeAntigravityModels(t *testing.T) {
	group := &Group{Platform: PlatformAnthropic}
	models := groupModelOptionsFromAccounts(group, nil)

	require.Contains(t, models, antigravity.DefaultModels()[0].ID)
}
