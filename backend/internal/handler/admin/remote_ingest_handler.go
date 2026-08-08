package admin

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type RemoteIngestHandler struct {
	service *service.RemoteIngestService
}

func NewRemoteIngestHandler(remoteService *service.RemoteIngestService) *RemoteIngestHandler {
	return &RemoteIngestHandler{service: remoteService}
}

type createRemoteRegistrationTokenRequest struct {
	ExpiresInSeconds int `json:"expires_in_seconds"`
}

type storedRemoteRegistrationToken struct {
	ID              string    `json:"id"`
	TokenCiphertext string    `json:"token_ciphertext"`
	Fingerprint     string    `json:"fingerprint"`
	ExpiresAt       time.Time `json:"expires_at"`
	CreatedAt       time.Time `json:"created_at"`
}

func (h *RemoteIngestHandler) CreateRegistrationToken(c *gin.Context) {
	var req createRemoteRegistrationTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid registration token request")
		return
	}
	payload := req
	result, err := executeAdminIdempotent(c, "admin.remote_ingest.registration_tokens.create", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		token, err := h.service.CreateRegistrationToken(ctx, time.Duration(req.ExpiresInSeconds)*time.Second)
		if err != nil {
			return nil, err
		}
		ciphertext, err := h.service.ProtectOneTimeSecret(token.Token)
		if err != nil {
			return nil, err
		}
		return storedRemoteRegistrationToken{
			ID:              token.ID,
			TokenCiphertext: ciphertext,
			Fingerprint:     token.Fingerprint,
			ExpiresAt:       token.ExpiresAt,
			CreatedAt:       token.CreatedAt,
		}, nil
	})
	if err != nil {
		if retryAfter := service.RetryAfterSecondsFromError(err); retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		response.ErrorFrom(c, err)
		return
	}
	if result == nil {
		response.ErrorFrom(c, errors.New("remote registration token result is unavailable"))
		return
	}
	if result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	raw, err := json.Marshal(result.Data)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var stored storedRemoteRegistrationToken
	if err := json.Unmarshal(raw, &stored); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	plaintext, err := h.service.RevealOneTimeSecret(stored.TokenCiphertext)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, service.RemoteRegistrationToken{
		ID:          stored.ID,
		Token:       plaintext,
		Fingerprint: stored.Fingerprint,
		ExpiresAt:   stored.ExpiresAt,
		CreatedAt:   stored.CreatedAt,
	})
}

func (h *RemoteIngestHandler) ListRegistrationTokens(c *gin.Context) {
	items, err := h.service.ListRegistrationTokens(c.Request.Context(), 500)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	filtered := items[:0]
	now := time.Now()
	for _, item := range items {
		itemStatus := "available"
		if item.UsedAt != nil {
			itemStatus = "used"
		} else if !item.ExpiresAt.After(now) {
			itemStatus = "expired"
		}
		if status == "" || status == itemStatus {
			filtered = append(filtered, item)
		}
	}
	page, pageSize := remotePagination(c)
	pageItems := paginateRemote(filtered, page, pageSize)
	response.Paginated(c, pageItems, int64(len(filtered)), page, pageSize)
}

func (h *RemoteIngestHandler) ListClients(c *gin.Context) {
	items, err := h.service.ListClients(c.Request.Context(), 500)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	search := strings.ToLower(strings.TrimSpace(c.Query("search")))
	status := strings.TrimSpace(c.Query("status"))
	filtered := items[:0]
	for _, item := range items {
		itemStatus := "active"
		if item.RevokedAt != nil {
			itemStatus = "revoked"
		}
		matchesSearch := search == "" || strings.Contains(strings.ToLower(item.ID+" "+item.MachineName+" "+item.AccessSubject+" "+item.PublicKeyFingerprint), search)
		if matchesSearch && (status == "" || status == itemStatus) {
			filtered = append(filtered, item)
		}
	}
	page, pageSize := remotePagination(c)
	response.Paginated(c, paginateRemote(filtered, page, pageSize), int64(len(filtered)), page, pageSize)
}

func (h *RemoteIngestHandler) RevokeClient(c *gin.Context) {
	id := c.Param("id")
	executeAdminIdempotentJSON(c, "admin.remote_ingest.clients.revoke", map[string]string{"client_id": id}, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		if err := h.service.RevokeClient(ctx, id); err != nil {
			return nil, err
		}
		return gin.H{"client_id": id, "revoked": true}, nil
	})
}

func (h *RemoteIngestHandler) ListDeliveries(c *gin.Context) {
	items, err := h.service.ListDeliveries(c.Request.Context(), c.Query("status"), 500)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	search := strings.ToLower(strings.TrimSpace(c.Query("search")))
	filtered := items[:0]
	for _, item := range items {
		if search == "" || strings.Contains(strings.ToLower(item.ID+" "+item.ExternalID+" "+item.ClientID+" "+item.Platform+" "+item.GroupName), search) {
			filtered = append(filtered, item)
		}
	}
	page, pageSize := remotePagination(c)
	response.Paginated(c, paginateRemote(filtered, page, pageSize), int64(len(filtered)), page, pageSize)
}

func (h *RemoteIngestHandler) RetryDelivery(c *gin.Context) {
	id := c.Param("id")
	executeAdminIdempotentJSON(c, "admin.remote_ingest.deliveries.retry", map[string]string{"delivery_id": id}, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		if err := h.service.RetryDelivery(ctx, id); err != nil {
			return nil, err
		}
		return gin.H{"delivery_id": id, "status": service.RemoteDeliveryPending}, nil
	})
}

func remotePagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func paginateRemote[T any](items []T, page, pageSize int) []T {
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []T{}
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}
