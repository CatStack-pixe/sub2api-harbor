//go:build unit

package service

import (
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAnthropicAPIKeyPassthroughExtra(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		accountType string
		extra       map[string]any
		want        any
		wantErrCode string
	}{
		{
			name:        "new Anthropic API key defaults to native Messages",
			platform:    PlatformAnthropic,
			accountType: AccountTypeAPIKey,
			want:        true,
		},
		{
			name:        "explicit false remains an opt out",
			platform:    PlatformAnthropic,
			accountType: AccountTypeAPIKey,
			extra:       map[string]any{anthropicAPIKeyPassthroughExtraKey: false},
			want:        false,
		},
		{
			name:        "explicit true remains enabled",
			platform:    PlatformAnthropic,
			accountType: AccountTypeAPIKey,
			extra:       map[string]any{anthropicAPIKeyPassthroughExtraKey: true},
			want:        true,
		},
		{
			name:        "other account types are unchanged",
			platform:    PlatformAnthropic,
			accountType: AccountTypeOAuth,
			extra:       map[string]any{"custom": "value"},
			want:        nil,
		},
		{
			name:        "malformed value is rejected",
			platform:    PlatformAnthropic,
			accountType: AccountTypeAPIKey,
			extra:       map[string]any{anthropicAPIKeyPassthroughExtraKey: "true"},
			wantErrCode: "ANTHROPIC_API_KEY_PASSTHROUGH_INVALID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeAnthropicAPIKeyPassthroughExtra(tt.platform, tt.accountType, tt.extra)
			if tt.wantErrCode != "" {
				require.Error(t, err)
				require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
				require.Equal(t, tt.wantErrCode, infraerrors.Reason(err))
				return
			}
			require.NoError(t, err)
			if tt.want == nil {
				require.Equal(t, tt.extra, got)
				return
			}
			require.Equal(t, tt.want, got[anthropicAPIKeyPassthroughExtraKey])
		})
	}
}
