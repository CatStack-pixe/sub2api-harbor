//go:build unit

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

func TestCompositeCustomModelsKeepExactRouteAliasesLiteral(t *testing.T) {
	const groupID int64 = 909
	routes := &gatewayModelsRouteRepoStub{routes: []service.CompositeModelRoute{{
		PublicModel: "route-only", TargetPlatform: service.PlatformOpenAI,
		UpstreamModel: "gpt-5.5", MatchType: service.CompositeRouteMatchExact,
		Endpoint: service.CompositeRouteEndpointAny, Enabled: true,
	}}}
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{}, routes)
	for _, codex := range []bool{false, true} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{
			ID: groupID, Platform: service.PlatformComposite,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true, Models: []string{"route-only", "route-only-thinking"},
			},
		}})
		if codex {
			h.CodexModels(c)
			var got codexModelsResponseForTest
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			require.Equal(t, []string{"route-only"}, codexModelSlugsForTest(got.Models))
		} else {
			h.Models(c)
			var got gatewayModelsResponseForTest
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			require.Equal(t, []string{"route-only"}, modelIDsForTest(got.Data))
		}
		require.Equal(t, http.StatusOK, rec.Code)
	}
	require.Equal(t, []string{"claude-opus-4-6-thinking"}, filterModelsByCustomListWithLiteralAliases(
		[]string{"claude-opus-4-6"}, nil, []string{"claude-opus-4-6-thinking"}, nil,
	))
}
