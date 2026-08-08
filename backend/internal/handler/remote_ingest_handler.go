package handler

import (
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const cloudflareAccessAssertionHeader = "Cf-Access-Jwt-Assertion"

type RemoteIngestHandler struct {
	service *service.RemoteIngestService
}

func NewRemoteIngestHandler(remoteService *service.RemoteIngestService) *RemoteIngestHandler {
	return &RemoteIngestHandler{service: remoteService}
}

type remoteEnrollRequest struct {
	RegistrationToken string `json:"registration_token" binding:"required"`
	MachineName       string `json:"machine_name" binding:"required"`
	PublicKey         string `json:"public_key" binding:"required"`
}

func (h *RemoteIngestHandler) Enroll(c *gin.Context) {
	subject, ok := h.verifyAccess(c)
	if !ok {
		return
	}
	var req remoteEnrollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid enrollment request")
		return
	}
	client, err := h.service.Enroll(c.Request.Context(), req.RegistrationToken, req.MachineName, req.PublicKey, subject)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, gin.H{
		"client_id":              client.ID,
		"public_key_fingerprint": client.PublicKeyFingerprint,
		"enrolled_at":            client.EnrolledAt,
	})
}

type remoteHandshakeRequest struct {
	ClientID string `json:"client_id" binding:"required"`
}

func (h *RemoteIngestHandler) Handshake(c *gin.Context) {
	subject, ok := h.verifyAccess(c)
	if !ok {
		return
	}
	var req remoteHandshakeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid handshake request")
		return
	}
	challenge, err := h.service.Handshake(c.Request.Context(), req.ClientID, subject)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, challenge)
}

func (h *RemoteIngestHandler) SubmitAccount(c *gin.Context) {
	subject, ok := h.verifyAccess(c)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 16*1024+1))
	if err != nil || len(body) > 16*1024 {
		response.Error(c, http.StatusRequestEntityTooLarge, "request body exceeds 16 KiB")
		return
	}
	delivery, queryToken, err := h.service.Submit(
		c.Request.Context(),
		c.GetHeader("X-Remote-Client-Id"),
		c.GetHeader("X-Remote-Challenge-Id"),
		c.GetHeader("X-Remote-Timestamp"),
		c.GetHeader("X-Remote-Signature"),
		body,
		subject,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, gin.H{
		"delivery_id": delivery.ID,
		"query_token": queryToken,
		"status":      delivery.Status,
	})
}

func (h *RemoteIngestHandler) GetDelivery(c *gin.Context) {
	subject, ok := h.verifyAccess(c)
	if !ok {
		return
	}
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	if !strings.HasPrefix(authorization, "Bearer ") {
		response.Unauthorized(c, "delivery query token is required")
		return
	}
	delivery, err := h.service.GetDeliveryAuthorized(c.Request.Context(), c.Param("id"), strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")), subject)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, delivery)
}

func (h *RemoteIngestHandler) verifyAccess(c *gin.Context) (string, bool) {
	if h == nil || h.service == nil || !h.service.Enabled() {
		response.ErrorFrom(c, service.ErrRemoteIngestDisabled)
		return "", false
	}
	subject, err := h.service.VerifyAccess(c.Request.Context(), c.GetHeader(cloudflareAccessAssertionHeader))
	if err != nil {
		response.ErrorFrom(c, err)
		return "", false
	}
	return subject, true
}
