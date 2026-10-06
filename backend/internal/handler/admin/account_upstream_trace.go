package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const upstreamTraceConfigExtraKey = "upstream_trace_config"
const upstreamTraceAccountExtraKey = "upstream_trace_account"

func boolPtr(value bool) *bool { return &value }

type CreateUpstreamTraceAccountRequest struct {
	Name            string         `json:"name" binding:"required"`
	Platform        string         `json:"platform" binding:"required"`
	Type            string         `json:"type" binding:"required,oneof=oauth apikey"`
	Credentials     map[string]any `json:"credentials"`
	UpstreamBaseURL string         `json:"upstream_base_url" binding:"required"`
	UpstreamGroupID *int64         `json:"upstream_group_id"`
	RequestModel    string         `json:"request_model"`
	Protocol        string         `json:"protocol"`
	Enabled         bool           `json:"enabled"`
}

type UpstreamTraceConfig struct {
	Enabled         bool   `json:"enabled"`
	UpstreamBaseURL string `json:"upstream_base_url,omitempty"`
	UpstreamGroupID *int64 `json:"upstream_group_id,omitempty"`
	RequestModel    string `json:"request_model,omitempty"`
	Protocol        string `json:"protocol,omitempty"`
	ExpectedGroup   string `json:"expected_group_name,omitempty"`
	Prompt          string `json:"prompt,omitempty"`
}

type upstreamTraceConfigRequest struct {
	Enabled         *bool  `json:"enabled"`
	UpstreamBaseURL string `json:"upstream_base_url"`
	UpstreamGroupID *int64 `json:"upstream_group_id"`
	RequestModel    string `json:"request_model"`
	Protocol        string `json:"protocol"`
	ExpectedGroup   string `json:"expected_group_name"`
	Prompt          string `json:"prompt"`
}

func (h *AccountHandler) GetUpstreamTraceConfig(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	config := UpstreamTraceConfig{}
	if account != nil && account.Extra != nil {
		if raw, ok := account.Extra[upstreamTraceConfigExtraKey].(map[string]any); ok {
			encoded, marshalErr := json.Marshal(raw)
			if marshalErr == nil {
				_ = json.Unmarshal(encoded, &config)
			}
		}
	}
	response.Success(c, config)
}

func (h *AccountHandler) CreateUpstreamTraceAccount(c *gin.Context) {
	var req CreateUpstreamTraceAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid test account: "+err.Error())
		return
	}
	if req.Type != service.AccountTypeOAuth && req.Type != service.AccountTypeAPIKey {
		response.BadRequest(c, "test account type must be oauth or apikey")
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.UpstreamBaseURL) == "" {
		response.BadRequest(c, "name and upstream_base_url are required")
		return
	}
	extra := map[string]any{
		upstreamTraceAccountExtraKey: true,
		upstreamTraceConfigExtraKey: map[string]any{
			"enabled":           req.Enabled,
			"upstream_base_url": strings.TrimSpace(req.UpstreamBaseURL),
			"upstream_group_id": req.UpstreamGroupID,
			"request_model":     strings.TrimSpace(req.RequestModel),
			"protocol":          strings.TrimSpace(req.Protocol),
		},
	}
	account, err := h.adminService.CreateAccount(c.Request.Context(), &service.CreateAccountInput{
		Name:              "[测试账户] " + strings.TrimSpace(req.Name),
		Platform:          strings.TrimSpace(req.Platform),
		Type:              req.Type,
		Credentials:       req.Credentials,
		Extra:             extra,
		SkipDefaultGroupBind: true,
		InitialSchedulable:  boolPtr(false),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"account": account,
		"enabled": req.Enabled,
		"account_type": req.Type,
		"test_account": true,
	})
}

func (h *AccountHandler) SaveUpstreamTraceConfig(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var req upstreamTraceConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid trace config: "+err.Error())
		return
	}
	if len(strings.TrimSpace(req.UpstreamBaseURL)) > 2048 ||
		len(strings.TrimSpace(req.RequestModel)) > 256 ||
		len(strings.TrimSpace(req.Prompt)) > 2000 {
		response.BadRequest(c, "trace config contains an oversized field")
		return
	}
	enabled := false
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	config := map[string]any{
		"enabled":             enabled,
		"upstream_base_url":   strings.TrimSpace(req.UpstreamBaseURL),
		"upstream_group_id":   req.UpstreamGroupID,
		"request_model":       strings.TrimSpace(req.RequestModel),
		"protocol":            strings.TrimSpace(req.Protocol),
		"expected_group_name": strings.TrimSpace(req.ExpectedGroup),
		"prompt":              strings.TrimSpace(req.Prompt),
	}
	if err := h.adminService.UpdateAccountExtra(c.Request.Context(), accountID, map[string]any{
		upstreamTraceConfigExtraKey: config,
	}); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}

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
		// Diagnostics are operator-facing: return the concrete validation or
		// transport failure instead of collapsing it into the generic
		// "internal error" envelope.
		response.Error(c, http.StatusBadRequest, err.Error())
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
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, result)
}
