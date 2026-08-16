package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const resendWebhookMaxBody = 1 << 20

type ResendWebhookHandler struct {
	settings   service.SettingRepository
	deliveries service.EmailDeliveryRepository
}

func NewResendWebhookHandler(settings service.SettingRepository, deliveries service.EmailDeliveryRepository) *ResendWebhookHandler {
	return &ResendWebhookHandler{settings: settings, deliveries: deliveries}
}

type resendWebhookPayload struct {
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	Data      struct {
		EmailID string   `json:"email_id"`
		To      []string `json:"to"`
	} `json:"data"`
}

func (h *ResendWebhookHandler) Handle(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, resendWebhookMaxBody+1))
	if err != nil || len(body) > resendWebhookMaxBody {
		response.Error(c, http.StatusBadRequest, "invalid webhook body")
		return
	}
	secret, err := h.settings.GetValue(c.Request.Context(), service.SettingKeyResendWebhookSecret)
	if err != nil || strings.TrimSpace(secret) == "" {
		response.Error(c, http.StatusServiceUnavailable, "webhook is not configured")
		return
	}
	if err := verifyResendWebhook(secret, c.GetHeader("svix-id"), c.GetHeader("svix-timestamp"), c.GetHeader("svix-signature"), body, time.Now()); err != nil {
		response.Error(c, http.StatusUnauthorized, "invalid webhook signature")
		return
	}
	var payload resendWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil || strings.TrimSpace(payload.Type) == "" {
		response.Error(c, http.StatusBadRequest, "invalid webhook payload")
		return
	}
	createdAt := time.Now().UTC()
	if parsed, parseErr := time.Parse(time.RFC3339Nano, payload.CreatedAt); parseErr == nil {
		createdAt = parsed
	}
	domain := ""
	if len(payload.Data.To) > 0 {
		parts := strings.Split(strings.ToLower(strings.TrimSpace(payload.Data.To[0])), "@")
		if len(parts) == 2 {
			domain = parts[1]
		}
	}
	_, err = h.deliveries.RecordResendEvent(c.Request.Context(), service.ResendDeliveryEvent{
		EventID: c.GetHeader("svix-id"), EventType: payload.Type, ProviderEmailID: payload.Data.EmailID,
		RecipientDomain: domain, EventCreatedAt: createdAt,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to record webhook")
		return
	}
	response.Success(c, gin.H{"received": true})
}

func (h *ResendWebhookHandler) ListDeliveries(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.deliveries.ListRecent(c.Request.Context(), limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list email deliveries")
		return
	}
	response.Success(c, gin.H{"items": items})
}

func verifyResendWebhook(secret, id, timestamp, signature string, body []byte, now time.Time) error {
	if id == "" || timestamp == "" || signature == "" {
		return errors.New("missing signature headers")
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || absDuration(now.Sub(time.Unix(seconds, 0))) > 5*time.Minute {
		return errors.New("stale webhook")
	}
	encoded := strings.TrimPrefix(strings.TrimSpace(secret), "whsec_")
	key, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		key, err = base64.StdEncoding.DecodeString(encoded)
	}
	if err != nil || len(key) == 0 {
		return errors.New("invalid webhook secret")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(id + "." + timestamp + "."))
	_, _ = mac.Write(body)
	want := mac.Sum(nil)
	for _, candidate := range strings.Fields(signature) {
		parts := strings.SplitN(candidate, ",", 2)
		if len(parts) != 2 || parts[0] != "v1" {
			continue
		}
		got, decodeErr := base64.StdEncoding.DecodeString(parts[1])
		if decodeErr != nil {
			got, decodeErr = base64.RawStdEncoding.DecodeString(parts[1])
		}
		if decodeErr == nil && subtle.ConstantTimeCompare(got, want) == 1 {
			return nil
		}
	}
	return errors.New("signature mismatch")
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
