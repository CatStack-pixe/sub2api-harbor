package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayModels_CustomAliasesIntersectGroupAndAPIKeyAllowlists(t *testing.T) {
	gin.SetMode(gin.TestMode)
	group := &service.Group{
		ID:       81,
		Platform: service.PlatformAgnes,
		ModelsListConfig: service.GroupModelsListConfig{
			Enabled:             true,
			Models:              []string{"public-flash", "public-pro", "hidden-alias"},
			ModelMappingEnabled: true,
			ModelMapping: map[string]string{
				"public-flash": "agnes-2.5-flash",
				"public-pro":   "agnes-2.5-pro-alpha",
				"hidden-alias": "agnes-2.0-flash",
			},
		},
		ModelAllowlist: service.GroupModelAllowlist{
			Enabled: true,
			Models:  []string{"public-*"},
		},
	}
	for _, tt := range []struct {
		name      string
		whitelist []string
		want      []string
	}{
		{name: "group policy keeps display order and public aliases", want: []string{"public-flash", "public-pro"}},
		{name: "api key further narrows aliases", whitelist: []string{"public-pro"}, want: []string{"public-pro"}},
		{name: "upstream mapped names do not bypass public policies", whitelist: []string{"agnes-2.5-pro-alpha"}, want: []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{})
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: group, ModelWhitelist: tt.whitelist})

			h.Models(c)

			require.Equal(t, http.StatusOK, rec.Code)
			var got gatewayModelsResponseForTest
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			require.Equal(t, tt.want, modelIDsForTest(got.Data))
		})
	}
}

func TestGatewayModels_RetrieveCannotExposeAliasExcludedByGroupAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models/hidden-alias", nil)
	c.Params = gin.Params{{Key: "model", Value: "hidden-alias"}}
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       82,
			Platform: service.PlatformAgnes,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled:             true,
				Models:              []string{"hidden-alias"},
				ModelMappingEnabled: true,
				ModelMapping:        map[string]string{"hidden-alias": "agnes-2.5-flash"},
			},
			ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"public-*"}},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "model_not_found")
}

func TestOpenAIModels_APIKeyFilteringPreservesMetadataAndConditionalIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		listKey  string
		modelKey string
		body     string
	}{
		{listKey: "data", modelKey: "id", body: `{"object":"list","data":[{"id":"allowed","owned_by":"provider","context":123},{"id":"hidden"}]}`},
		{listKey: "models", modelKey: "slug", body: `{"models":[{"slug":"allowed","display_name":"Provider Model","context_window":123},{"slug":"hidden"}],"extra":"preserved"}`},
	} {
		t.Run(tt.listKey, func(t *testing.T) {
			manifest := &service.OpenAIModelsResponse{Body: []byte(tt.body), ETag: "broader-catalogue"}
			key := &service.APIKey{ModelWhitelist: []string{"allowed"}}
			request := func(etag string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodGet, "/models", nil)
				c.Request.Header.Set("If-None-Match", etag)
				writeAPIKeyFilteredOpenAIModelsResponse(c, manifest, key, tt.listKey, tt.modelKey)
				return rec
			}
			first := request(manifest.ETag)
			require.Equal(t, http.StatusOK, first.Code)
			require.NotContains(t, first.Body.String(), "hidden")
			require.Contains(t, first.Body.String(), "123")
			require.NotEqual(t, manifest.ETag, first.Header().Get("ETag"))
			require.Equal(t, tt.body, string(manifest.Body), "shared catalogue must remain intact")
			require.Equal(t, "broader-catalogue", manifest.ETag)
			second := request(first.Header().Get("ETag"))
			require.Equal(t, http.StatusNotModified, second.Code)
			require.Empty(t, second.Body.String())
		})
	}
}
