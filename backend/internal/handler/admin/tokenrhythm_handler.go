package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type resolveTokenRhythmSessionRequest struct {
	Sess    string `json:"sess" binding:"required"`
	ProxyID *int64 `json:"proxy_id"`
}

type createTokenRhythmAPIKeyRequest struct {
	Sess      string `json:"sess"`
	Cookie    string `json:"cookie"`
	Name      string `json:"name" binding:"required"`
	ProxyID   *int64 `json:"proxy_id"`
	AccountID *int64 `json:"account_id"`
}

type tokenRhythmAPIKeyCredentialRequest struct {
	Sess      string `json:"sess"`
	Cookie    string `json:"cookie"`
	ProxyID   *int64 `json:"proxy_id"`
	AccountID *int64 `json:"account_id"`
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
	if req.AccountID == nil && strings.TrimSpace(req.Sess) == "" && strings.TrimSpace(req.Cookie) == "" {
		response.BadRequest(c, "TokenRhythm sess or Cookie is required")
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

	var result *service.TokenRhythmAPIKeyResult
	var err error
	if req.AccountID != nil {
		result, err = h.accountTestService.CreateTokenRhythmAPIKeyForAccount(c.Request.Context(), *req.AccountID, strings.TrimSpace(req.Name), proxyURL)
	} else {
		result, err = h.accountTestService.CreateTokenRhythmAPIKeyWithCredential(c.Request.Context(), strings.TrimSpace(req.Sess), strings.TrimSpace(req.Cookie), strings.TrimSpace(req.Name), proxyURL)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// ListTokenRhythmAPIKeys returns the provider's masked API key inventory.
// The request body carries the session/cookie so credentials never enter URLs.
func (h *AccountHandler) ListTokenRhythmAPIKeys(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req tokenRhythmAPIKeyCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid TokenRhythm API key list request")
		return
	}
	if req.AccountID == nil && strings.TrimSpace(req.Sess) == "" && strings.TrimSpace(req.Cookie) == "" {
		response.BadRequest(c, "TokenRhythm sess or Cookie is required")
		return
	}
	if h.accountTestService == nil {
		response.BadRequest(c, "TokenRhythm API key service is not enabled")
		return
	}
	proxyURL, ok := h.tokenRhythmProxyURL(c, req.ProxyID)
	if !ok {
		return
	}
	var result *service.TokenRhythmAPIKeyListResult
	var err error
	if req.AccountID != nil {
		result, err = h.accountTestService.ListTokenRhythmAPIKeysForAccount(c.Request.Context(), *req.AccountID, proxyURL)
	} else {
		result, err = h.accountTestService.ListTokenRhythmAPIKeys(c.Request.Context(), strings.TrimSpace(req.Sess), strings.TrimSpace(req.Cookie), proxyURL)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *AccountHandler) DisableTokenRhythmAPIKey(c *gin.Context) {
	h.mutateTokenRhythmAPIKey(c, "disable")
}

func (h *AccountHandler) DeleteTokenRhythmAPIKey(c *gin.Context) {
	h.mutateTokenRhythmAPIKey(c, "delete")
}

func (h *AccountHandler) mutateTokenRhythmAPIKey(c *gin.Context, action string) {
	c.Header("Cache-Control", "no-store")
	var req tokenRhythmAPIKeyCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid TokenRhythm API key request")
		return
	}
	if req.AccountID == nil && strings.TrimSpace(req.Sess) == "" && strings.TrimSpace(req.Cookie) == "" {
		response.BadRequest(c, "TokenRhythm sess or Cookie is required")
		return
	}
	if h.accountTestService == nil {
		response.BadRequest(c, "TokenRhythm API key service is not enabled")
		return
	}
	proxyURL, ok := h.tokenRhythmProxyURL(c, req.ProxyID)
	if !ok {
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	var result *service.TokenRhythmAPIKeyActionResult
	var err error
	if req.AccountID != nil {
		if action == "disable" {
			result, err = h.accountTestService.DisableTokenRhythmAPIKeyForAccount(c.Request.Context(), *req.AccountID, id, proxyURL)
		} else {
			result, err = h.accountTestService.DeleteTokenRhythmAPIKeyForAccount(c.Request.Context(), *req.AccountID, id, proxyURL)
		}
	} else if action == "disable" {
		result, err = h.accountTestService.DisableTokenRhythmAPIKey(c.Request.Context(), strings.TrimSpace(req.Sess), strings.TrimSpace(req.Cookie), id, proxyURL)
	} else {
		result, err = h.accountTestService.DeleteTokenRhythmAPIKey(c.Request.Context(), strings.TrimSpace(req.Sess), strings.TrimSpace(req.Cookie), id, proxyURL)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *AccountHandler) tokenRhythmProxyURL(c *gin.Context, proxyID *int64) (string, bool) {
	if proxyID == nil {
		return "", true
	}
	if h.adminService == nil {
		response.BadRequest(c, "TokenRhythm proxy service is not enabled")
		return "", false
	}
	proxy, err := h.adminService.GetProxy(c.Request.Context(), *proxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return "", false
	}
	if proxy == nil || !proxy.IsActive() {
		response.BadRequest(c, "TokenRhythm proxy is unavailable")
		return "", false
	}
	return proxy.URL(), true
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
