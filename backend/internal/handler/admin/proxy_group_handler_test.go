package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type proxyGroupAdminServiceStub struct {
	groups       []service.ProxyGroup
	createdName  string
	updatedID    int64
	updatedName  string
	deletedID    int64
	batchIDs     []int64
	batchGroupID *int64
}

func (s *proxyGroupAdminServiceStub) ListProxyGroups(context.Context) ([]service.ProxyGroup, error) {
	return s.groups, nil
}

func (s *proxyGroupAdminServiceStub) CreateProxyGroup(_ context.Context, name string) (*service.ProxyGroup, error) {
	s.createdName = name
	return &service.ProxyGroup{ID: 10, Name: name}, nil
}

func (s *proxyGroupAdminServiceStub) UpdateProxyGroup(_ context.Context, id int64, name string) (*service.ProxyGroup, error) {
	s.updatedID = id
	s.updatedName = name
	return &service.ProxyGroup{ID: id, Name: name}, nil
}

func (s *proxyGroupAdminServiceStub) DeleteProxyGroup(_ context.Context, id int64) error {
	s.deletedID = id
	return nil
}

func (s *proxyGroupAdminServiceStub) GetOrCreateProxyGroupByName(_ context.Context, name string) (*service.ProxyGroup, error) {
	return &service.ProxyGroup{ID: 10, Name: name}, nil
}

func (s *proxyGroupAdminServiceStub) BatchGroupProxies(_ context.Context, ids []int64, groupID *int64) (int64, error) {
	s.batchIDs = append([]int64(nil), ids...)
	if groupID != nil {
		value := *groupID
		s.batchGroupID = &value
	}
	return int64(len(ids)), nil
}

func TestParseProxyListFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rawQuery  string
		wantGroup *int64
		ungrouped bool
		wantError string
	}{
		{name: "group", rawQuery: "group_id=7", wantGroup: proxyGroupInt64(7)},
		{name: "ungrouped", rawQuery: "ungrouped=true", ungrouped: true},
		{name: "mutually exclusive", rawQuery: "group_id=7&ungrouped=true", wantError: "cannot be used together"},
		{name: "invalid group", rawQuery: "group_id=0", wantError: "invalid proxy group ID"},
		{name: "invalid boolean", rawQuery: "ungrouped=sometimes", wantError: "invalid ungrouped value"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/?"+tc.rawQuery, nil)
			filters, err := parseProxyListFilters(ctx)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantGroup, filters.ProxyGroupID)
			require.Equal(t, tc.ungrouped, filters.Ungrouped)
		})
	}
}

func TestProxyGroupCRUDHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupService := &proxyGroupAdminServiceStub{
		groups: []service.ProxyGroup{{ID: 1, Name: "Primary", TotalCount: 2, ActiveCount: 1, InactiveCount: 1}},
	}
	handler := &ProxyHandler{proxyGroupService: groupService}
	router := gin.New()
	router.GET("/proxy-groups", handler.ListProxyGroups)
	router.POST("/proxy-groups", handler.CreateProxyGroup)
	router.PUT("/proxy-groups/:id", handler.UpdateProxyGroup)
	router.DELETE("/proxy-groups/:id", handler.DeleteProxyGroup)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/proxy-groups", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"total_count":2`)

	response = performProxyGroupJSONRequest(router, http.MethodPost, "/proxy-groups", map[string]any{"name": "New"})
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "New", groupService.createdName)

	response = performProxyGroupJSONRequest(router, http.MethodPut, "/proxy-groups/9", map[string]any{"name": "Renamed"})
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, int64(9), groupService.updatedID)
	require.Equal(t, "Renamed", groupService.updatedName)

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/proxy-groups/9", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, int64(9), groupService.deletedID)
}

func TestProxyBatchGroupHandlerAcceptsUngrouped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupService := &proxyGroupAdminServiceStub{}
	handler := &ProxyHandler{proxyGroupService: groupService}
	router := gin.New()
	router.POST("/proxies/batch-group", handler.BatchGroup)

	response := performProxyGroupJSONRequest(router, http.MethodPost, "/proxies/batch-group", map[string]any{
		"ids":            []int64{3, 2, 3},
		"proxy_group_id": nil,
	})
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, []int64{3, 2, 3}, groupService.batchIDs)
	require.Nil(t, groupService.batchGroupID)

	var envelope struct {
		Data struct {
			Updated int64 `json:"updated"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Equal(t, int64(3), envelope.Data.Updated)
}

func TestProxyCreateAndUpdatePassProxyGroupAssignment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminService := newProxyGroupAwareAdminService()
	handler := NewProxyHandler(adminService)
	router := gin.New()
	router.POST("/proxies", handler.Create)
	router.PUT("/proxies/:id", handler.Update)

	response := performProxyGroupJSONRequest(router, http.MethodPost, "/proxies", map[string]any{
		"name": "grouped-proxy", "protocol": "http", "host": "127.0.0.1", "port": 8080,
		"proxy_group_id": 9,
	})
	require.Equal(t, http.StatusOK, response.Code)
	require.Len(t, adminService.createdProxies, 1)
	require.Equal(t, int64(9), *adminService.createdProxies[0].ProxyGroupID)

	response = performProxyGroupJSONRequest(router, http.MethodPut, "/proxies/400", map[string]any{
		"name": "moved-proxy", "protocol": "http", "host": "127.0.0.1", "port": 8080,
		"proxy_group_id": 10,
	})
	require.Equal(t, http.StatusOK, response.Code)
	require.Len(t, adminService.updatedProxies, 1)
	require.Equal(t, int64(10), *adminService.updatedProxies[0].ProxyGroupID)
	require.True(t, adminService.updatedProxies[0].ProxyGroupIDSet)

	response = performProxyGroupJSONRequest(router, http.MethodPut, "/proxies/400", map[string]any{
		"name": "ungrouped-proxy", "protocol": "http", "host": "127.0.0.1", "port": 8080,
		"proxy_group_id": nil,
	})
	require.Equal(t, http.StatusOK, response.Code)
	require.Len(t, adminService.updatedProxies, 2)
	require.Nil(t, adminService.updatedProxies[1].ProxyGroupID)
	require.True(t, adminService.updatedProxies[1].ProxyGroupIDSet)

	response = performProxyGroupJSONRequest(router, http.MethodPut, "/proxies/400", map[string]any{
		"status": "inactive",
	})
	require.Equal(t, http.StatusOK, response.Code)
	require.Len(t, adminService.updatedProxies, 3)
	require.False(t, adminService.updatedProxies[2].ProxyGroupIDSet)
}

func performProxyGroupJSONRequest(router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	payload, _ := json.Marshal(body)
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func proxyGroupInt64(value int64) *int64 {
	return &value
}
