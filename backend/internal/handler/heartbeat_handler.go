package handler

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const heartbeatRequestBodyLimit = 1 << 20

type HeartbeatHandler struct {
	provisioning *service.HeartbeatProvisioningService
}

func NewHeartbeatHandler(provisioning *service.HeartbeatProvisioningService) *HeartbeatHandler {
	return &HeartbeatHandler{provisioning: provisioning}
}

type heartbeatRequest struct {
	SessionKey string         `json:"session_key"`
	Timestamp  int64          `json:"ts"`
	Keys       []heartbeatKey `json:"keys"`
}

type heartbeatKey struct {
	Fingerprint string  `json:"fp"`
	Provider    string  `json:"provider"`
	Balance     float64 `json:"balance"`
	CheckedAt   string  `json:"checked_at"`
}

func (h *HeartbeatHandler) Handle(c *gin.Context) {
	if h == nil || h.provisioning == nil || !h.provisioning.Enabled() {
		c.Status(http.StatusNotFound)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, heartbeatRequestBodyLimit)
	defer func() { _ = c.Request.Body.Close() }()
	var request heartbeatRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid heartbeat"})
		return
	}
	if request.Timestamp <= 0 || len(strings.TrimSpace(request.SessionKey)) != 32 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid heartbeat"})
		return
	}
	if _, err := hex.DecodeString(strings.TrimSpace(request.SessionKey)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid heartbeat"})
		return
	}
	keys := make([]service.HeartbeatKeyInput, 0, len(request.Keys))
	for _, key := range request.Keys {
		checkedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(key.CheckedAt))
		if err != nil {
			checkedAt = time.Time{}
		}
		keys = append(keys, service.HeartbeatKeyInput{
			Fingerprint: key.Fingerprint,
			Provider:    key.Provider,
			Balance:     key.Balance,
			CheckedAt:   checkedAt,
		})
	}
	sourceIP := c.ClientIP()
	accepted, err := h.provisioning.Queue(c.Request.Context(), sourceIP, request.SessionKey, time.Unix(request.Timestamp, 0), keys)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrHeartbeatUnauthorized):
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		case errors.Is(err, service.ErrHeartbeatInvalidPayload):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid heartbeat"})
		case errors.Is(err, service.ErrHeartbeatRateLimited):
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "heartbeat rate limited"})
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "heartbeat queue unavailable"})
		}
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"accepted": accepted})
}
