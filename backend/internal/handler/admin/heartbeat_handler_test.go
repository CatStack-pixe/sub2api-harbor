package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestHeartbeatHandlerGetLogsReturnsPaginatedData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, err := service.NewHeartbeatProvisioningService(&config.Config{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewHeartbeatProvisioningService() error = %v", err)
	}
	h := NewHeartbeatHandler(svc)
	r := gin.New()
	r.GET("/logs", h.GetLogs)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logs?page=2&page_size=999", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Body.String(); !containsHeartbeatHandlerText(got, `"page":2`) || !containsHeartbeatHandlerText(got, `"page_size":200`) {
		t.Fatalf("pagination missing from response: %s", got)
	}
}

func TestHeartbeatHandlerGetLogsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHeartbeatHandler(nil)
	r := gin.New()
	r.GET("/logs", h.GetLogs)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func containsHeartbeatHandlerText(body, want string) bool {
	for i := 0; i+len(want) <= len(body); i++ {
		if body[i:i+len(want)] == want {
			return true
		}
	}
	return false
}
