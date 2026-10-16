package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayOpenAIModelDirectoryDefaultsRespectAPIKeyRestrictions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name      string
		whitelist []string
		want      []string
	}{
		{name: "unrestricted", want: openai.DefaultModelIDs()},
		{name: "restricted", whitelist: []string{"gpt-5.4"}, want: []string{"gpt-5.4"}},
		{name: "no matching models", whitelist: []string{"not-a-default-model"}, want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &service.Group{ID: 217, Platform: service.PlatformOpenAI}
			h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
				byGroup: map[int64][]service.Account{
					group.ID: {{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}},
				},
			})
			apiKey := &service.APIKey{GroupID: &group.ID, Group: group, ModelWhitelist: tc.whitelist}
			request := func(modelID string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
				if modelID != "" {
					c.Params = gin.Params{{Key: "model", Value: modelID}}
				}
				c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
				h.Models(c)
				return rec
			}

			// Repeat discovery to cover the service's cached nil fallback.
			for range 2 {
				rec := request("")
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				var got gatewayModelsResponseForTest
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
				require.Equal(t, tc.want, modelIDsForTest(got.Data))
			}
			for _, modelID := range []string{"gpt-5.4", "gpt-5.5"} {
				wantStatus := http.StatusNotFound
				for _, visible := range tc.want {
					if visible == modelID {
						wantStatus = http.StatusOK
					}
				}
				rec := request(modelID)
				require.Equal(t, wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestGatewayOpenAICustomDirectoryRetirementAndMixedProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oauth := service.Account{
		ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.5": "gpt-5.5"}},
	}
	for _, tc := range []struct {
		name      string
		accounts  []service.Account
		selected  []string
		whitelist []string
		allowlist []string
		want      []string
	}{
		{
			name:     "default fallback keeps selection order",
			accounts: []service.Account{{ID: 2, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}},
			selected: []string{"gpt-5.5", "missing-model", "gpt-5.4"},
			want:     []string{"gpt-5.5", "gpt-5.4"},
		},
		{
			name:      "default fallback respects API key restriction",
			accounts:  []service.Account{{ID: 2, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}},
			selected:  []string{"gpt-5.5", "gpt-5.4"},
			whitelist: []string{"gpt-5.4"},
			want:      []string{"gpt-5.4"},
		},
		{
			name:     "OAuth-only empty directory",
			accounts: []service.Account{oauth},
			selected: []string{"gpt-5.5", "gpt-5.4"},
			want:     []string{},
		},
		{
			name:      "API key and group selections cannot revive retirement",
			accounts:  []service.Account{oauth},
			selected:  []string{"gpt-5.5"},
			whitelist: []string{"gpt-5.5"},
			allowlist: []string{"gpt-5.5"},
			want:      []string{},
		},
		{
			name: "OpenAI API key still serves retired subscription model",
			accounts: []service.Account{oauth, {
				ID: 2, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
				Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.5": "gpt-5.5"}},
			}},
			selected: []string{"gpt-5.5"},
			want:     []string{"gpt-5.5"},
		},
		{
			name: "compatible pool keeps aliases and selection order",
			accounts: []service.Account{oauth, {
				ID: 2, Platform: service.PlatformChatAnywhere, Type: service.AccountTypeAPIKey,
				Credentials: map[string]any{"model_mapping": map[string]any{
					"gpt-5.5": "gpt-5.5", "public-legacy": "gpt-5.5", "hidden-legacy": "gpt-5.5",
				}},
			}},
			selected:  []string{"public-legacy", "missing-model", "gpt-5.5", "hidden-legacy"},
			whitelist: []string{"public-legacy", "gpt-5.5"},
			want:      []string{"public-legacy", "gpt-5.5"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &service.Group{
				ID: 218, Platform: service.PlatformOpenAI,
				ModelsListConfig: service.GroupModelsListConfig{Enabled: true, Models: tc.selected},
				ModelAllowlist:   service.GroupModelAllowlist{Enabled: len(tc.allowlist) > 0, Models: tc.allowlist},
			}
			h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
				byGroup: map[int64][]service.Account{group.ID: tc.accounts},
			})
			apiKey := &service.APIKey{GroupID: &group.ID, Group: group, ModelWhitelist: tc.whitelist}
			for _, codex := range []bool{false, true} {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
				c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
				if codex {
					h.CodexModels(c)
					var got codexModelsResponseForTest
					require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
					require.Equal(t, tc.want, codexModelSlugsForTest(got.Models))
				} else {
					h.Models(c)
					var got gatewayModelsResponseForTest
					require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
					require.Equal(t, tc.want, modelIDsForTest(got.Data))
				}
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestGatewayModelDirectoryFiltersPreserveNilAndEmpty(t *testing.T) {
	apiKey := &service.APIKey{ModelWhitelist: []string{"gpt-5.5"}}
	require.Nil(t, filterAPIKeyModels(apiKey, nil))
	for _, source := range [][]string{{}, {"gpt-5.4"}} {
		filtered := filterAPIKeyModels(apiKey, source)
		require.NotNil(t, filtered)
		require.Empty(t, filtered)
	}

	fallback := []string{"gpt-5.5"}
	require.Equal(t, fallback, filterModelsByCustomListForPlatform(service.PlatformOpenAI, nil, fallback, fallback))
	filtered := filterModelsByCustomListForPlatform(service.PlatformOpenAI, []string{}, fallback, fallback)
	require.NotNil(t, filtered)
	require.Empty(t, filtered)
}
