package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAllowsModel(t *testing.T) {
	tests := []struct {
		name      string
		whitelist []string
		model     string
		want      bool
	}{
		{name: "empty whitelist allows every model", model: "any-model", want: true},
		{name: "exact match", whitelist: []string{"deepseek-ai/deepseek-v4-pro"}, model: "deepseek-ai/deepseek-v4-pro", want: true},
		{name: "exact mismatch", whitelist: []string{"deepseek-ai/deepseek-v4-pro"}, model: "deepseek-ai/deepseek-v4-flash", want: false},
		{name: "prefix wildcard", whitelist: []string{"deepseek-ai/*"}, model: "deepseek-ai/deepseek-v4-flash", want: true},
		{name: "prefix wildcard mismatch", whitelist: []string{"deepseek-ai/*"}, model: "meta/llama-3.1-8b", want: false},
		{name: "global wildcard", whitelist: []string{"*"}, model: "any-model", want: true},
		{name: "blank model is rejected when restricted", whitelist: []string{"model"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := &APIKey{ModelWhitelist: tt.whitelist}
			require.Equal(t, tt.want, key.AllowsModel(tt.model))
		})
	}
}

func TestNormalizeAPIKeyModelWhitelist(t *testing.T) {
	models, err := normalizeAPIKeyModelWhitelist([]string{
		" deepseek-ai/deepseek-v4-pro ",
		"deepseek-ai/deepseek-v4-pro",
		"deepseek-ai/*",
		"",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"deepseek-ai/deepseek-v4-pro", "deepseek-ai/*"}, models)

	for _, invalid := range [][]string{
		{"deep*seek"},
		{"deepseek**"},
		{string(make([]byte, maxAPIKeyModelPatternBytes+1))},
	} {
		_, err = normalizeAPIKeyModelWhitelist(invalid)
		require.ErrorIs(t, err, ErrAPIKeyInvalidModelPattern)
	}
}
