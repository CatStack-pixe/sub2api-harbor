package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// GetKimiBalance probes the official Kimi balance endpoint without changing
// account scheduling state.
func (h *AccountHandler) GetKimiBalance(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.accountTestService == nil {
		response.BadRequest(c, "Kimi balance service is not enabled")
		return
	}
	result, err := h.accountTestService.FetchKimiBalance(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
