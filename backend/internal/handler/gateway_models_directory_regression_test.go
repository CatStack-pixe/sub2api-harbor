package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
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

func modelDirectoryHTTPRouterForTest(h *GatewayHandler, apiKey *service.APIKey) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
		c.Next()
	})
	models := func(c *gin.Context) {
		if c.Query("client_version") != "" {
			h.CodexModels(c)
			return
		}
		h.Models(c)
	}
	router.GET("/v1/models", models)
	router.GET("/models", models)
	router.GET("/v1/models/:model", h.Models)
	router.GET("/backend-api/codex/models", h.CodexModels)
	return router
}

func requestDirectoryHTTPForTest(router *gin.Engine, path, etag string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("If-None-Match", etag)
	router.ServeHTTP(rec, req)
	return rec
}

func requireDirectoryHTTPModelsForTest(t *testing.T, router *gin.Engine, path string, want []string) {
	t.Helper()
	rec := requestDirectoryHTTPForTest(router, path, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	if path == "/v1/models" {
		var got gatewayModelsResponseForTest
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.NotNil(t, got.Data, rec.Body.String())
		require.Equal(t, want, modelIDsForTest(got.Data))
		return
	}
	var got codexModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotNil(t, got.Models, rec.Body.String())
	require.Equal(t, want, codexModelSlugsForTest(got.Models))
	etag := rec.Header().Get("ETag")
	require.NotEmpty(t, etag)
	cached := requestDirectoryHTTPForTest(router, path, etag)
	require.Equal(t, http.StatusNotModified, cached.Code, cached.Body.String())
	require.Empty(t, cached.Body.String())
}

func TestGatewayModelDirectoryRetirementAfterSelectionHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oauth := service.Account{
		ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"legacy-*": "gpt-6.1-sol", "legacy-retired": "gpt-5.5", "gpt-5.5": "gpt-5.5",
		}},
	}
	api := service.Account{
		ID: 2, Platform: service.PlatformChatAnywhere, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"legacy-retired": "gpt-5.5", "gpt-5.5": "gpt-5.5",
		}},
	}
	replacement := oauth
	replacement.Credentials = map[string]any{"model_mapping": map[string]any{
		"legacy-*": "gpt-6.1-sol", "legacy-retired": "gpt-6.1-sol", "gpt-5.5": "gpt-6.1-sol",
	}}
	excluded := api
	excluded.Credentials = map[string]any{"model_mapping": map[string]any{"different-model": "gpt-5.4"}}
	unrelated := api
	unrelated.Platform = service.PlatformAnthropic
	shadow := oauth
	shadow.ID = 3
	shadow.Type = service.AccountTypeAPIKey
	shadow.ParentAccountID = &oauth.ID
	selection := []string{"legacy-retired", "legacy-live", "gpt-5.5"}
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformChatAnywhere} {
		for _, mode := range []string{"custom", "allowlist", "combined"} {
			for _, tc := range []struct {
				name      string
				accounts  []service.Account
				whitelist []string
				want      []string
			}{
				{name: "wildcard cannot revive exact retirement", accounts: []service.Account{oauth}, want: []string{"legacy-live"}},
				{name: "shadow still uses subscription", accounts: []service.Account{shadow}, want: []string{"legacy-live"}},
				{name: "compatible API key serves retired names", accounts: []service.Account{oauth, api}, want: selection},
				{name: "compatible API key first", accounts: []service.Account{api, oauth}, want: selection},
				{name: "excluding API key cannot rescue", accounts: []service.Account{oauth, excluded}, want: []string{"legacy-live"}},
				{name: "unrelated API key cannot rescue", accounts: []service.Account{oauth, unrelated}, want: []string{"legacy-live"}},
				{name: "replacement target keeps public names", accounts: []service.Account{replacement}, want: selection},
				{name: "key restricts mixed pool", accounts: []service.Account{oauth, api}, whitelist: []string{"legacy-live"}, want: []string{"legacy-live"}},
				{name: "key permits only retired alias", accounts: []service.Account{oauth}, whitelist: []string{"legacy-retired"}, want: []string{}},
				{name: "key has no matching names", accounts: []service.Account{oauth, api}, whitelist: []string{"missing"}, want: []string{}},
			} {
				t.Run(platform+"/"+mode+"/"+tc.name, func(t *testing.T) {
					group := &service.Group{ID: 219, Platform: platform}
					if mode != "allowlist" {
						group.ModelsListConfig = service.GroupModelsListConfig{Enabled: true, Models: selection}
					}
					if mode != "custom" {
						group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: selection}
					}
					h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
						byGroup: map[int64][]service.Account{group.ID: tc.accounts},
					})
					router := modelDirectoryHTTPRouterForTest(h, &service.APIKey{
						GroupID: &group.ID, Group: group, ModelWhitelist: tc.whitelist,
					})
					paths := []string{"/v1/models"}
					// Official OpenAI groups use the live manifest handler in
					// production; compatible groups use this generated handler.
					if platform != service.PlatformOpenAI {
						paths = append(paths, "/models?client_version=0.150.0", "/backend-api/codex/models")
					}
					for range 2 {
						for _, path := range paths {
							requireDirectoryHTTPModelsForTest(t, router, path, tc.want)
						}
						for _, modelID := range selection {
							wantStatus := http.StatusNotFound
							for _, visible := range tc.want {
								if modelID == visible {
									wantStatus = http.StatusOK
								}
							}
							got := requestDirectoryHTTPForTest(router, "/v1/models/"+modelID, "")
							require.Equal(t, wantStatus, got.Code, got.Body.String())
						}
					}
				})
			}
		}
	}
}

func TestGatewayCompatibleModelDirectoryRetirementHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oauth := service.Account{
		ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"gpt-5.5": "gpt-5.5", "legacy-retired": "gpt-5.5",
		}},
	}
	api := oauth
	api.ID, api.Type = 2, service.AccountTypeAPIKey
	platforms := domain.PlatformIDsWhere(func(spec domain.PlatformSpec) bool {
		return domain.UsesOpenAIGateway(spec.ID)
	})
	require.Contains(t, platforms, service.PlatformChatAnywhere)
	for _, platform := range platforms {
		for _, tc := range []struct {
			name      string
			accounts  []service.Account
			whitelist []string
			selected  []string
			allowlist []string
			want      []string
		}{
			{name: "authoritative empty", accounts: []service.Account{oauth}, want: []string{}},
			{name: "custom authoritative empty", accounts: []service.Account{oauth}, selected: []string{"gpt-5.5", "legacy-retired"}, want: []string{}},
			{name: "allowlist authoritative empty", accounts: []service.Account{oauth}, allowlist: []string{"gpt-5.5", "legacy-retired"}, want: []string{}},
			{name: "mixed API pool", accounts: []service.Account{oauth, api}, want: []string{"gpt-5.5", "legacy-retired"}},
			{name: "key restricts mixed API pool", accounts: []service.Account{oauth, api}, whitelist: []string{"legacy-retired"}, want: []string{"legacy-retired"}},
		} {
			t.Run(platform+"/"+tc.name, func(t *testing.T) {
				group := &service.Group{
					ID: 220, Platform: platform,
					ModelsListConfig: service.GroupModelsListConfig{Enabled: len(tc.selected) > 0, Models: tc.selected},
					ModelAllowlist:   service.GroupModelAllowlist{Enabled: len(tc.allowlist) > 0, Models: tc.allowlist},
				}
				h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
					byGroup: map[int64][]service.Account{group.ID: tc.accounts},
				})
				router := modelDirectoryHTTPRouterForTest(h, &service.APIKey{
					GroupID: &group.ID, Group: group, ModelWhitelist: tc.whitelist,
				})
				for range 2 {
					requireDirectoryHTTPModelsForTest(t, router, "/v1/models", tc.want)
					if platform != service.PlatformOpenAI {
						requireDirectoryHTTPModelsForTest(t, router, "/models?client_version=0.150.0", tc.want)
					}
					got := requestDirectoryHTTPForTest(router, "/v1/models/legacy-retired", "")
					wantStatus := http.StatusNotFound
					if len(tc.want) > 0 {
						wantStatus = http.StatusOK
					}
					require.Equal(t, wantStatus, got.Code, got.Body.String())
				}
			})
		}
	}
}

func TestGatewayCompatibleModelDirectoryDefaultFallbackHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oauth := service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth}
	api := service.Account{ID: 2, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}
	for _, mode := range []string{"plain", "custom", "allowlist"} {
		for _, tc := range []struct {
			name      string
			accounts  []service.Account
			whitelist []string
			want      []string
		}{
			{name: "subscription filters defaults", accounts: []service.Account{oauth}, whitelist: []string{"gpt-5.5", "gpt-5.4"}, want: []string{"gpt-5.4"}},
			{name: "API pool retains defaults", accounts: []service.Account{oauth, api}, whitelist: []string{"gpt-5.5", "gpt-5.4"}, want: []string{"gpt-5.5", "gpt-5.4"}},
			{name: "key permits no defaults", accounts: []service.Account{oauth, api}, whitelist: []string{"missing"}, want: []string{}},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				group := &service.Group{ID: 223, Platform: service.PlatformTokenRhythm}
				if mode == "custom" {
					group.ModelsListConfig = service.GroupModelsListConfig{Enabled: true, Models: []string{"gpt-5.5", "gpt-5.4"}}
				}
				if mode == "allowlist" {
					group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.5", "gpt-5.4"}}
				}
				h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
					byGroup: map[int64][]service.Account{group.ID: tc.accounts},
				})
				require.Nil(t, h.gatewayService.GetAvailableModels(context.Background(), &group.ID, group.Platform))
				router := modelDirectoryHTTPRouterForTest(h, &service.APIKey{
					GroupID: &group.ID, Group: group, ModelWhitelist: tc.whitelist,
				})
				for range 2 {
					requireDirectoryHTTPModelsForTest(t, router, "/v1/models", tc.want)
					requireDirectoryHTTPModelsForTest(t, router, "/models?client_version=0.150.0", tc.want)
				}
			})
		}
	}
}

func TestGatewayAgnesModelDirectoryRequestMappingRetirementHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oauth := service.Account{
		ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"gpt-5.5": "gpt-5.5", "gpt-6.1-sol": "gpt-6.1-sol",
		}},
	}
	api := oauth
	api.ID, api.Type = 2, service.AccountTypeAPIKey
	for _, tc := range []struct {
		name      string
		accounts  []service.Account
		selected  []string
		whitelist []string
		allowlist []string
		want      []string
	}{
		{name: "group targets are filtered", accounts: []service.Account{oauth}, want: []string{"gpt-5.5", "public-live"}},
		{name: "custom keeps replacement and order", accounts: []service.Account{oauth}, selected: []string{"public-live", "public-retired", "gpt-5.5"}, want: []string{"public-live", "gpt-5.5"}},
		{name: "API key permits retired group target", accounts: []service.Account{oauth, api}, selected: []string{"public-retired", "gpt-5.5"}, want: []string{"public-retired", "gpt-5.5"}},
		{name: "client key sees public names", accounts: []service.Account{oauth, api}, whitelist: []string{"public-retired"}, want: []string{"public-retired"}},
		{name: "client key cannot revive group target", accounts: []service.Account{oauth}, selected: []string{"public-retired"}, whitelist: []string{"public-retired"}, want: []string{}},
		{name: "allowlist cannot revive group target", accounts: []service.Account{oauth}, allowlist: []string{"public-retired"}, want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &service.Group{
				ID: 224, Platform: service.PlatformAgnes,
				ModelsListConfig: service.GroupModelsListConfig{
					Enabled: len(tc.selected) > 0, Models: tc.selected, ModelMappingEnabled: true,
					ModelMapping: map[string]string{
						"public-retired": "gpt-5.5", "public-live": "gpt-6.1-sol", "gpt-5.5": "gpt-6.1-sol",
					},
				},
				ModelAllowlist: service.GroupModelAllowlist{Enabled: len(tc.allowlist) > 0, Models: tc.allowlist},
			}
			h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
				byGroup: map[int64][]service.Account{group.ID: tc.accounts},
			})
			router := modelDirectoryHTTPRouterForTest(h, &service.APIKey{
				GroupID: &group.ID, Group: group, ModelWhitelist: tc.whitelist,
			})
			for range 2 {
				requireDirectoryHTTPModelsForTest(t, router, "/v1/models", tc.want)
				requireDirectoryHTTPModelsForTest(t, router, "/models?client_version=0.150.0", tc.want)
				wantStatus := http.StatusNotFound
				for _, modelID := range tc.want {
					if modelID == "public-retired" {
						wantStatus = http.StatusOK
					}
				}
				got := requestDirectoryHTTPForTest(router, "/v1/models/public-retired", "")
				require.Equal(t, wantStatus, got.Code, got.Body.String())
			}
		})
	}
}

type changingModelDirectoryAccountRepo struct {
	*gatewayModelsAccountRepoStub
	initial []service.Account
	later   []service.Account
	reads   int
}

func (r *changingModelDirectoryAccountRepo) ListSchedulableByGroupID(_ context.Context, _ int64) ([]service.Account, error) {
	r.reads++
	accounts := r.later
	if r.reads == 1 {
		accounts = r.initial
	}
	return append([]service.Account(nil), accounts...), nil
}

func TestGatewayModelDirectoryAccountSnapshotHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oauth := service.Account{
		ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"legacy-*": "gpt-6.1-sol", "legacy-retired": "gpt-5.5",
		}},
	}
	api := service.Account{
		ID: 2, Platform: service.PlatformChatAnywhere, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{"legacy-retired": "gpt-5.5"}},
	}
	for _, mode := range []string{"custom", "allowlist"} {
		for _, initiallySupported := range []bool{false, true} {
			name := mode + "/retired then supported"
			initial, later := []service.Account{oauth}, []service.Account{oauth, api}
			want := []string{}
			if initiallySupported {
				name = mode + "/supported then retired"
				initial, later = later, initial
				want = []string{"legacy-retired"}
			}
			t.Run(name, func(t *testing.T) {
				group := &service.Group{ID: 221, Platform: service.PlatformChatAnywhere}
				if mode == "custom" {
					group.ModelsListConfig = service.GroupModelsListConfig{Enabled: true, Models: []string{"legacy-retired"}}
				} else {
					group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"legacy-retired"}}
				}
				repo := &changingModelDirectoryAccountRepo{
					gatewayModelsAccountRepoStub: &gatewayModelsAccountRepoStub{
						byGroup: map[int64][]service.Account{group.ID: later},
					},
					initial: initial, later: later,
				}
				h := newGatewayModelsHandlerForTest(repo)
				router := modelDirectoryHTTPRouterForTest(h, &service.APIKey{GroupID: &group.ID, Group: group})
				requireDirectoryHTTPModelsForTest(t, router, "/v1/models", want)
				require.Equal(t, 1, repo.reads, "selection must reuse the directory account pool")
				requireDirectoryHTTPModelsForTest(t, router, "/v1/models", want)
				require.Equal(t, 1, repo.reads, "cache hits must keep models and accounts together")
				requireDirectoryHTTPModelsForTest(t, router, "/models?client_version=0.150.0", want)
				requireDirectoryHTTPModelsForTest(t, router, "/v1/models", want)
				h.gatewayService.InvalidateAvailableModelsCache(&group.ID, group.Platform)
				nextWant := []string{"legacy-retired"}
				if initiallySupported {
					nextWant = []string{}
				}
				requireDirectoryHTTPModelsForTest(t, router, "/v1/models", nextWant)
			})
		}
	}
}
