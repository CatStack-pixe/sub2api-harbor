package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// GetDeepSeekBalance probes the provider's balance endpoint without changing
// account scheduling state. Provider HTTP status codes are preserved by the
// service so the UI can distinguish authentication, payment, and rate-limit
// failures.
func (h *AccountHandler) GetDeepSeekBalance(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.accountTestService == nil {
		response.BadRequest(c, "DeepSeek balance service is not enabled")
		return
	}
	result, err := h.accountTestService.FetchDeepSeekBalance(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
