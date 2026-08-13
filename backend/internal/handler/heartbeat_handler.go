package handler

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const heartbeatRequestBodyLimit = 1 << 20

const heartbeatLegacyTimestampLayout = "2006-01-02T15:04:05.999999999"

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
	Fingerprint      string  `json:"fp"`
	Provider         string  `json:"provider"`
	Balance          float64 `json:"balance"`
	CheckedAt        string  `json:"checked_at"`
	BalanceCheckedAt string  `json:"balance_checked_at"`
}

func parseHeartbeatCheckedAt(value string) (time.Time, error) {
	raw := strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed, nil
	}
	// Older key-checker clients emitted UTC timestamps without an explicit
	// offset. Keep accepting that wire format while making the interpretation
	// deterministic and documenting RFC3339 as the preferred format.
	if parsed, err := time.ParseInLocation(heartbeatLegacyTimestampLayout, raw, time.UTC); err == nil {
		return parsed, nil
	}
	return time.Time{}, errors.New("invalid heartbeat checked_at")
}

func (key heartbeatKey) parseCheckedAt() (time.Time, error) {
	checkedAt := strings.TrimSpace(key.CheckedAt)
	balanceCheckedAt := strings.TrimSpace(key.BalanceCheckedAt)
	if checkedAt != "" && balanceCheckedAt != "" && checkedAt != balanceCheckedAt {
		return time.Time{}, errors.New("conflicting heartbeat checked_at values")
	}
	if checkedAt == "" {
		checkedAt = balanceCheckedAt
	}
	return parseHeartbeatCheckedAt(checkedAt)
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
		checkedAt, err := key.parseCheckedAt()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid heartbeat"})
			return
		}
		keys = append(keys, service.HeartbeatKeyInput{
			Fingerprint: key.Fingerprint,
			Provider:    key.Provider,
			Balance:     key.Balance,
			CheckedAt:   checkedAt,
		})
	}
	// Heartbeats are source-IP restricted. Resolve the IP only through Gin's
	// configured trusted-proxy chain; never use compatibility forwarding headers.
	sourceIP := ip.GetSecurityClientIP(c, false)
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
