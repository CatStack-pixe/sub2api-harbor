package admin

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type HeartbeatHandler struct {
	provisioning *service.HeartbeatProvisioningService
}

type heartbeatTargetRequest struct {
	GroupID      int64 `json:"group_id"`
	ProxyGroupID int64 `json:"proxy_group_id"`
}

type heartbeatConfigRequest struct {
	Enabled              bool                     `json:"enabled"`
	VaultURL             string                   `json:"vault_url"`
	AllowInsecureVault   bool                     `json:"allow_insecure_vault"`
	AllowedSourceIPs     []string                 `json:"allowed_source_ips"`
	DefaultGroupID       int64                    `json:"default_group_id"`
	Targets              []heartbeatTargetRequest `json:"targets"`
	WorkerCount          int                      `json:"worker_count"`
	ProxyProbeWorkers    int                      `json:"proxy_probe_workers"`
	ProxyProbeSampleSize int                      `json:"proxy_probe_sample_size"`
	ProxyProbeTimeoutS   int                      `json:"proxy_probe_timeout_seconds"`
	ProxySweepTTLSecond  int                      `json:"proxy_sweep_ttl_seconds"`
	MaxAttempts          int                      `json:"max_attempts"`
}

type heartbeatTargetResponse struct {
	GroupID      int64 `json:"group_id"`
	ProxyGroupID int64 `json:"proxy_group_id"`
}

type heartbeatConfigResponse struct {
	Enabled              bool                                 `json:"enabled"`
	VaultURL             string                               `json:"vault_url"`
	AllowInsecureVault   bool                                 `json:"allow_insecure_vault"`
	AllowedSourceIPs     []string                             `json:"allowed_source_ips"`
	DefaultGroupID       int64                                `json:"default_group_id"`
	Targets              []heartbeatTargetResponse            `json:"targets"`
	WorkerCount          int                                  `json:"worker_count"`
	ProxyProbeWorkers    int                                  `json:"proxy_probe_workers"`
	ProxyProbeSampleSize int                                  `json:"proxy_probe_sample_size"`
	ProxyProbeTimeoutS   int                                  `json:"proxy_probe_timeout_seconds"`
	ProxySweepTTLSecond  int                                  `json:"proxy_sweep_ttl_seconds"`
	MaxAttempts          int                                  `json:"max_attempts"`
	ConfigSource         string                               `json:"config_source"`
	Status               *service.HeartbeatProvisioningStatus `json:"status,omitempty"`
}

func NewHeartbeatHandler(provisioning *service.HeartbeatProvisioningService) *HeartbeatHandler {
	return &HeartbeatHandler{provisioning: provisioning}
}

func (h *HeartbeatHandler) GetConfig(c *gin.Context) {
	if h == nil || h.provisioning == nil {
		response.Error(c, http.StatusServiceUnavailable, "Heartbeat service not available")
		return
	}
	payload, err := h.configResponse(c, true)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to get heartbeat configuration")
		return
	}
	response.Success(c, payload)
}

func (h *HeartbeatHandler) UpdateConfig(c *gin.Context) {
	if h == nil || h.provisioning == nil {
		response.Error(c, http.StatusServiceUnavailable, "Heartbeat service not available")
		return
	}
	var request heartbeatConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid heartbeat configuration")
		return
	}
	requested := config.HeartbeatProvisioningConfig{
		Enabled:              request.Enabled,
		VaultURL:             request.VaultURL,
		AllowInsecureVault:   request.AllowInsecureVault,
		AllowedSourceIPs:     request.AllowedSourceIPs,
		DefaultGroupID:       request.DefaultGroupID,
		WorkerCount:          request.WorkerCount,
		ProxyProbeWorkers:    request.ProxyProbeWorkers,
		ProxyProbeSampleSize: request.ProxyProbeSampleSize,
		ProxyProbeTimeoutS:   request.ProxyProbeTimeoutS,
		ProxySweepTTLSecond:  request.ProxySweepTTLSecond,
		MaxAttempts:          request.MaxAttempts,
	}
	requested.Targets = make([]config.HeartbeatProvisioningTarget, 0, len(request.Targets))
	for _, target := range request.Targets {
		requested.Targets = append(requested.Targets, config.HeartbeatProvisioningTarget{GroupID: target.GroupID, ProxyGroupID: target.ProxyGroupID})
	}
	if err := h.provisioning.UpdateConfig(c.Request.Context(), requested); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	payload, err := h.configResponse(c, true)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Heartbeat configuration applied but could not be read back")
		return
	}
	response.Success(c, payload)
}

func (h *HeartbeatHandler) GetOptions(c *gin.Context) {
	if h == nil || h.provisioning == nil {
		response.Error(c, http.StatusServiceUnavailable, "Heartbeat service not available")
		return
	}
	options, err := h.provisioning.Options(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to get heartbeat options")
		return
	}
	response.Success(c, options)
}

func (h *HeartbeatHandler) GetStatus(c *gin.Context) {
	if h == nil || h.provisioning == nil {
		response.Error(c, http.StatusServiceUnavailable, "Heartbeat service not available")
		return
	}
	status, err := h.provisioning.Status(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to get heartbeat status")
		return
	}
	response.Success(c, status)
}

// GetLogs returns recent heartbeat provisioning jobs for the admin page.
func (h *HeartbeatHandler) GetLogs(c *gin.Context) {
	if h == nil || h.provisioning == nil {
		response.Error(c, http.StatusServiceUnavailable, "Heartbeat service not available")
		return
	}
	page, pageSize := response.ParsePagination(c)
	if pageSize > 200 {
		pageSize = 200
	}
	logs, err := h.provisioning.ListLogs(c.Request.Context(), page, pageSize)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to get heartbeat logs")
		return
	}
	if logs == nil {
		response.Paginated(c, []*service.HeartbeatProvisioningLog{}, 0, page, pageSize)
		return
	}
	response.Paginated(c, logs.Logs, int64(logs.Total), logs.Page, logs.PageSize)
}

func (h *HeartbeatHandler) configResponse(c *gin.Context, includeStatus bool) (*heartbeatConfigResponse, error) {
	cfg, source := h.provisioning.ConfigSnapshot()
	payload := &heartbeatConfigResponse{
		Enabled:              cfg.Enabled,
		VaultURL:             cfg.VaultURL,
		AllowInsecureVault:   cfg.AllowInsecureVault,
		AllowedSourceIPs:     append([]string(nil), cfg.AllowedSourceIPs...),
		DefaultGroupID:       cfg.DefaultGroupID,
		Targets:              make([]heartbeatTargetResponse, 0, len(cfg.Targets)),
		WorkerCount:          cfg.WorkerCount,
		ProxyProbeWorkers:    cfg.ProxyProbeWorkers,
		ProxyProbeSampleSize: cfg.ProxyProbeSampleSize,
		ProxyProbeTimeoutS:   cfg.ProxyProbeTimeoutS,
		ProxySweepTTLSecond:  cfg.ProxySweepTTLSecond,
		MaxAttempts:          cfg.MaxAttempts,
		ConfigSource:         source,
	}
	for _, target := range cfg.Targets {
		payload.Targets = append(payload.Targets, heartbeatTargetResponse{GroupID: target.GroupID, ProxyGroupID: target.ProxyGroupID})
	}
	if includeStatus {
		status, err := h.provisioning.Status(c.Request.Context())
		if err != nil {
			return nil, err
		}
		payload.Status = status
	}
	return payload, nil
}
