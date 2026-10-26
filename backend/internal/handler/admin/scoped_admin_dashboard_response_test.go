//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type scopedDashboardUsageRepository struct {
	service.UsageLogRepository
}

func (scopedDashboardUsageRepository) GetUserUsageTrend(context.Context, time.Time, time.Time, string, int, string) ([]usagestats.UserUsageTrendPoint, error) {
	return []usagestats.UserUsageTrendPoint{{
		Date:       "2026-10-25",
		UserID:     7,
		Email:      "private-dashboard@example.invalid",
		Username:   "private-dashboard-user",
		Requests:   2,
		Tokens:     30,
		Cost:       1.25,
		ActualCost: 1,
	}}, nil
}

func (scopedDashboardUsageRepository) GetUserSpendingRanking(context.Context, time.Time, time.Time, int) (*usagestats.UserSpendingRankingResponse, error) {
	return &usagestats.UserSpendingRankingResponse{
		Ranking: []usagestats.UserSpendingRankingItem{{
			UserID:     7,
			Email:      "private-dashboard@example.invalid",
			Username:   "private-dashboard-user",
			ActualCost: 1,
			Requests:   2,
			Tokens:     30,
		}},
		TotalActualCost: 1,
		TotalRequests:   2,
		TotalTokens:     30,
	}, nil
}

func (scopedDashboardUsageRepository) GetUserBreakdownStats(context.Context, time.Time, time.Time, usagestats.UserBreakdownDimension, int) ([]usagestats.UserBreakdownItem, error) {
	return []usagestats.UserBreakdownItem{{
		UserID:      7,
		Email:       "private-dashboard@example.invalid",
		Requests:    2,
		TotalTokens: 30,
		ActualCost:  1,
	}}, nil
}

func resetScopedDashboardPrivacyCaches() {
	dashboardUsersTrendCache = newSnapshotCache(5 * time.Minute)
	dashboardUsersRankingCache = newSnapshotCache(5 * time.Minute)
	dashboardSnapshotV2Cache = newSnapshotCache(5 * time.Minute)
}

func scopedDashboardPrivacyRouter(permissions []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	svc := service.NewDashboardService(scopedDashboardUsageRepository{}, nil, nil, nil)
	handler := NewDashboardHandler(svc, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Header("Vary", "Origin")
		c.Next()
	})
	router.Use(func(c *gin.Context) {
		if permissions != nil {
			c.Request = c.Request.WithContext(adminaccess.WithPrincipal(
				c.Request.Context(),
				&adminaccess.Principal{Policy: adminaccess.Policy{GlobalPermissions: permissions}},
			))
		}
		c.Next()
	})
	router.GET("/admin/dashboard/users-trend", handler.GetUserUsageTrend)
	router.GET("/admin/dashboard/users-ranking", handler.GetUserSpendingRanking)
	router.GET("/admin/dashboard/user-breakdown", handler.GetUserBreakdown)
	router.GET("/admin/dashboard/snapshot-v2", handler.GetSnapshotV2)
	return router
}

func TestScopedAdminDashboardUserContactProjection(t *testing.T) {
	t.Cleanup(resetScopedDashboardPrivacyCaches)
	for _, identity := range []struct {
		name        string
		permissions []string
		redacted    bool
	}{
		{name: "dashboard_only", permissions: []string{"dashboard.read"}, redacted: true},
		{name: "dashboard_and_users", permissions: []string{"dashboard.read", "users.read"}},
		{name: "admin_jwt"},
	} {
		t.Run(identity.name, func(t *testing.T) {
			resetScopedDashboardPrivacyCaches()
			router := scopedDashboardPrivacyRouter(identity.permissions)
			for _, path := range []string{
				"/admin/dashboard/users-trend?start_date=2026-10-25&end_date=2026-10-26",
				"/admin/dashboard/users-ranking?start_date=2026-10-25&end_date=2026-10-26",
				"/admin/dashboard/user-breakdown?start_date=2026-10-25&end_date=2026-10-26&group_id=1",
				"/admin/dashboard/snapshot-v2?start_date=2026-10-25&end_date=2026-10-26&include_stats=false&include_trend=false&include_model_stats=false&include_group_stats=false&include_users_trend=true",
			} {
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
				require.Equal(t, http.StatusOK, recorder.Code, path+": "+recorder.Body.String())
				require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"), path)
				require.Contains(t, recorder.Header().Get("Vary"), "Origin", path)
				require.Contains(t, recorder.Header().Get("Vary"), "Authorization", path)
				require.Contains(t, recorder.Header().Get("Vary"), "X-API-Key", path)
				if identity.redacted {
					require.NotContains(t, recorder.Body.String(), "private-dashboard@example.invalid", path)
					require.NotContains(t, recorder.Body.String(), "private-dashboard-user", path)
					require.Contains(t, recorder.Body.String(), `"user_id":7`, path)
				} else {
					require.Contains(t, recorder.Body.String(), "private-dashboard@example.invalid", path)
					if path != "/admin/dashboard/user-breakdown?start_date=2026-10-25&end_date=2026-10-26&group_id=1" {
						require.Contains(t, recorder.Body.String(), "private-dashboard-user", path)
					}
				}
			}
		})
	}
}

func TestScopedAdminDashboardResponsesRedactSharedCache(t *testing.T) {
	t.Cleanup(resetScopedDashboardPrivacyCaches)
	resetScopedDashboardPrivacyCaches()
	adminRouter := scopedDashboardPrivacyRouter(nil)
	scopedRouter := scopedDashboardPrivacyRouter([]string{"dashboard.read"})
	paths := []string{
		"/admin/dashboard/users-trend?start_date=2026-10-25&end_date=2026-10-26",
		"/admin/dashboard/users-ranking?start_date=2026-10-25&end_date=2026-10-26",
		"/admin/dashboard/user-breakdown?start_date=2026-10-25&end_date=2026-10-26&group_id=1",
		"/admin/dashboard/snapshot-v2?start_date=2026-10-25&end_date=2026-10-26&include_stats=false&include_trend=false&include_model_stats=false&include_group_stats=false&include_users_trend=true",
	}
	for _, path := range paths {
		adminResponse := httptest.NewRecorder()
		adminRouter.ServeHTTP(adminResponse, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, adminResponse.Code, path+": "+adminResponse.Body.String())
		require.Contains(t, adminResponse.Body.String(), "private-dashboard@example.invalid", path)
	}
	for _, path := range paths {
		scopedResponse := httptest.NewRecorder()
		scopedRouter.ServeHTTP(scopedResponse, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, scopedResponse.Code, path+": "+scopedResponse.Body.String())
		require.NotContains(t, scopedResponse.Body.String(), "private-dashboard@example.invalid", path)
		require.NotContains(t, scopedResponse.Body.String(), "private-dashboard-user", path)
		require.Contains(t, scopedResponse.Body.String(), `"user_id":7`, path)
	}
}

func TestScopedAdminDashboardSnapshotUsesRedactedETagForSharedCache(t *testing.T) {
	t.Cleanup(resetScopedDashboardPrivacyCaches)
	resetScopedDashboardPrivacyCaches()
	adminRouter := scopedDashboardPrivacyRouter(nil)
	path := "/admin/dashboard/snapshot-v2?start_date=2026-10-25&end_date=2026-10-26&include_stats=false&include_trend=false&include_model_stats=false&include_group_stats=false&include_users_trend=true"

	adminResponse := httptest.NewRecorder()
	adminRouter.ServeHTTP(adminResponse, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, adminResponse.Code)
	adminETag := adminResponse.Header().Get("ETag")
	require.NotEmpty(t, adminETag)
	require.Contains(t, adminResponse.Body.String(), "private-dashboard@example.invalid")

	scopedRouter := scopedDashboardPrivacyRouter([]string{"dashboard.read"})
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("If-None-Match", adminETag)
	scopedResponse := httptest.NewRecorder()
	scopedRouter.ServeHTTP(scopedResponse, request)

	require.Equal(t, http.StatusOK, scopedResponse.Code)
	require.NotEqual(t, adminETag, scopedResponse.Header().Get("ETag"))
	require.NotContains(t, scopedResponse.Body.String(), "private-dashboard@example.invalid")
	require.NotContains(t, scopedResponse.Body.String(), "private-dashboard-user")
	require.Contains(t, scopedResponse.Body.String(), `"user_id":7`)
}
