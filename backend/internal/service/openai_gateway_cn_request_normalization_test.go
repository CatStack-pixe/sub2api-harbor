//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeCNChatCompletionsStopField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		body        string
		wantChanged bool
		wantStop    string
		wantExists  bool
	}{
		{
			name:        "single string becomes array",
			body:        `{"model":"glm-5.3","stop":"</block>"}`,
			wantChanged: true,
			wantStop:    `["</block>"]`,
			wantExists:  true,
		},
		{
			name:        "empty string becomes empty array",
			body:        `{"stop":""}`,
			wantChanged: true,
			wantStop:    `[]`,
			wantExists:  true,
		},
		{
			name:        "array remains unchanged",
			body:        `{"stop":["</block>","END"]}`,
			wantChanged: false,
			wantStop:    `["</block>","END"]`,
			wantExists:  true,
		},
		{
			name:        "null is omitted",
			body:        `{"stop":null}`,
			wantChanged: true,
			wantExists:  false,
		},
		{
			name:        "absent remains absent",
			body:        `{"model":"glm-5.3"}`,
			wantChanged: false,
			wantExists:  false,
		},
		{
			name:        "unsupported scalar is preserved",
			body:        `{"stop":123}`,
			wantChanged: false,
			wantStop:    `123`,
			wantExists:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed, err := normalizeCNChatCompletionsStopField([]byte(tt.body))
			require.NoError(t, err)
			require.Equal(t, tt.wantChanged, changed)
			stop := gjson.GetBytes(got, "stop")
			require.Equal(t, tt.wantExists, stop.Exists())
			if tt.wantExists {
				require.JSONEq(t, tt.wantStop, stop.Raw)
			}
		})
	}
}

func TestShouldNormalizeCNChatCompletionsStop(t *testing.T) {
	t.Parallel()

	require.True(t, shouldNormalizeCNChatCompletionsStop(&Account{Platform: PlatformZhipu}))
	require.True(t, shouldNormalizeCNChatCompletionsStop(&Account{Platform: PlatformGLM}))
	require.False(t, shouldNormalizeCNChatCompletionsStop(&Account{Platform: PlatformDeepSeek}))
	require.False(t, shouldNormalizeCNChatCompletionsStop(&Account{Platform: PlatformOpenAI}))
	require.False(t, shouldNormalizeCNChatCompletionsStop(nil))
}
