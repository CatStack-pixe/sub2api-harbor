package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type resolveTokenRhythmSessionRequest struct {
	Sess    string `json:"sess" binding:"required"`
	ProxyID *int64 `json:"proxy_id"`
}

type createTokenRhythmAPIKeyRequest struct {
	Sess    string `json:"sess" binding:"required"`
	Name    string `json:"name" binding:"required"`
	ProxyID *int64 `json:"proxy_id"`
}

// ResolveTokenRhythmSession exchanges a TokenRhythm sess value for the
// minimal account Cookie and the current referral link. Nothing is persisted.
func (h *AccountHandler) ResolveTokenRhythmSession(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req resolveTokenRhythmSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid TokenRhythm session request")
		return
	}
	if h.accountTestService == nil {
		response.BadRequest(c, "TokenRhythm session service is not enabled")
		return
	}

	proxyURL := ""
	if req.ProxyID != nil {
		if h.adminService == nil {
			response.BadRequest(c, "TokenRhythm proxy service is not enabled")
			return
		}
		proxy, err := h.adminService.GetProxy(c.Request.Context(), *req.ProxyID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if proxy == nil || !proxy.IsActive() {
			response.BadRequest(c, "TokenRhythm proxy is unavailable")
			return
		}
		proxyURL = proxy.URL()
	}

	result, err := h.accountTestService.ResolveTokenRhythmSession(c.Request.Context(), strings.TrimSpace(req.Sess), proxyURL)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// CreateTokenRhythmAPIKey creates a provider API key from a short-lived sess.
// The sess is used only for this request and is never persisted.
func (h *AccountHandler) CreateTokenRhythmAPIKey(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req createTokenRhythmAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid TokenRhythm API key request")
		return
	}
	if h.accountTestService == nil {
		response.BadRequest(c, "TokenRhythm API key service is not enabled")
		return
	}

	proxyURL := ""
	if req.ProxyID != nil {
		if h.adminService == nil {
			response.BadRequest(c, "TokenRhythm proxy service is not enabled")
			return
		}
		proxy, err := h.adminService.GetProxy(c.Request.Context(), *req.ProxyID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if proxy == nil || !proxy.IsActive() {
			response.BadRequest(c, "TokenRhythm proxy is unavailable")
			return
		}
		proxyURL = proxy.URL()
	}

	result, err := h.accountTestService.CreateTokenRhythmAPIKey(
		c.Request.Context(),
		strings.TrimSpace(req.Sess),
		strings.TrimSpace(req.Name),
		proxyURL,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// GetTokenRhythmBalance probes the provider usage endpoint without changing
// account scheduling state.
func (h *AccountHandler) GetTokenRhythmBalance(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.accountTestService == nil {
		response.BadRequest(c, "TokenRhythm balance service is not enabled")
		return
	}
	result, err := h.accountTestService.FetchTokenRhythmBalance(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
