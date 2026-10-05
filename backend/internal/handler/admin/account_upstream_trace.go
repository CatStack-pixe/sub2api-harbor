package admin

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// TraceUpstream runs one explicit, non-streaming upstream diagnostic.
// POST /api/v1/admin/accounts/:id/upstream-trace
func (h *AccountHandler) TraceUpstream(c *gin.Context) {
	if h == nil || h.accountTestService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Account test service is unavailable")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var req service.UpstreamTraceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid trace request: "+err.Error())
		return
	}
	result, err := h.accountTestService.RunUpstreamTrace(c.Request.Context(), accountID, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// ProbeUpstreamAuthorization runs the explicit POST /keys group_id regression.
// The route is step-up protected in routes/admin.go.
func (h *AccountHandler) ProbeUpstreamAuthorization(c *gin.Context) {
	if h == nil || h.accountTestService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Account test service is unavailable")
		return
	}
	var req service.UpstreamAuthorizationProbeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid authorization probe request: "+err.Error())
		return
	}
	result, err := h.accountTestService.RunUpstreamAuthorizationProbe(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
