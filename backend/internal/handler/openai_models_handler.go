package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *GatewayHandler) pinnedOpenAIModels(c *gin.Context, apiKey *service.APIKey) {
	group := apiKey.Group
	if c.Request.Context().Err() != nil {
		return
	}
	if h.openAIGatewayService == nil {
		writeOpenAIModelsError(c, http.StatusInternalServerError, "api_error", "OpenAI model discovery is not configured")
		return
	}
	etag := c.GetHeader("If-None-Match")
	if c.Param("model") != "" || len(apiKey.ModelWhitelist) > 0 {
		etag = "" // A collection ETag cannot validate a single-model representation.
	}
	response, account, err := h.openAIGatewayService.FetchPinnedOpenAIModelsList(
		c.Request.Context(), group, h.maxAccountSwitches, etag,
	)
	if c.Request.Context().Err() != nil {
		return
	}
	if err != nil {
		if errors.Is(err, service.ErrNoPinnedCodexModelsAccounts) {
			writeOpenAIModelsError(c, http.StatusServiceUnavailable, "upstream_error", "No available OpenAI model discovery accounts")
			return
		}
		writeOpenAIModelsError(c, infraerrors.Code(err), "upstream_error", infraerrors.Message(err))
		return
	}
	setOpsSelectedAccount(c, account.ID, account.Platform)
	writeAPIKeyFilteredOpenAIModelsResponse(c, response, apiKey, "data", "id")
}

// Filtering is applied after group/account projection, before conditional
// validation, so a key cannot reuse a broader catalogue through an old ETag.
func writeAPIKeyFilteredOpenAIModelsResponse(c *gin.Context, manifest *service.OpenAIModelsResponse, apiKey *service.APIKey, listKey, modelKey string) {
	if apiKey == nil || len(apiKey.ModelWhitelist) == 0 {
		writeOpenAIModelsResponse(c, manifest)
		return
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(manifest.Body, &envelope); err != nil || envelope == nil {
		writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Invalid model catalogue")
		return
	}
	var models []json.RawMessage
	if raw, exists := envelope[listKey]; !exists || json.Unmarshal(raw, &models) != nil {
		writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Invalid model catalogue")
		return
	}
	kept := make([]json.RawMessage, 0, len(models))
	for _, raw := range models {
		var model map[string]json.RawMessage
		var id string
		if json.Unmarshal(raw, &model) == nil && json.Unmarshal(model[modelKey], &id) == nil && apiKey.AllowsModel(id) {
			kept = append(kept, raw)
		}
	}
	envelope[listKey], _ = json.Marshal(kept)
	body, err := json.Marshal(envelope)
	if err != nil {
		writeOpenAIModelsError(c, http.StatusInternalServerError, "api_error", "Failed to encode model catalogue")
		return
	}
	filtered := *manifest
	filtered.Body = body
	filtered.ETag = service.CodexModelsManifestETag(body)
	filtered.NotModified = c.Param("model") == "" && service.CodexModelsManifestETagMatches(c.GetHeader("If-None-Match"), filtered.ETag)
	writeOpenAIModelsResponse(c, &filtered)
}

func writeOpenAIModelsError(c *gin.Context, status int, errorType, message string) {
	c.JSON(status, gin.H{"error": gin.H{"type": errorType, "message": message}})
}

func writeOpenAIModelsResponse(c *gin.Context, manifest *service.OpenAIModelsResponse) {
	if c.Param("model") != "" {
		writeRetrievedModel(c, manifest.Body)
		return
	}
	if manifest.ETag != "" {
		c.Header("ETag", manifest.ETag)
	}
	if manifest.NotModified {
		c.Status(http.StatusNotModified)
		c.Writer.WriteHeaderNow()
		return
	}
	c.Data(http.StatusOK, "application/json", manifest.Body)
}

// Both discovery endpoints consume the same final catalogue, after group/platform
// selection and allowlist filtering. Preserve every field on the selected entry.
func writeModelsListResponse(c *gin.Context, models any) {
	response := gin.H{"object": "list", "data": models}
	if c.Param("model") == "" {
		c.JSON(http.StatusOK, response)
		return
	}
	body, err := json.Marshal(response)
	if err != nil {
		writeOpenAIModelsError(c, http.StatusInternalServerError, "api_error", "Failed to encode model catalogue")
		return
	}
	writeRetrievedModel(c, body)
}

func writeRetrievedModel(c *gin.Context, body []byte) {
	var catalog struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &catalog); err != nil {
		writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Invalid model catalogue")
		return
	}
	modelID := c.Param("model")
	for _, raw := range catalog.Data {
		var model map[string]json.RawMessage
		if err := json.Unmarshal(raw, &model); err != nil {
			writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Invalid model catalogue entry")
			return
		}
		var id string
		if err := json.Unmarshal(model["id"], &id); err != nil {
			writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Invalid model catalogue ID")
			return
		}
		if id == modelID {
			c.Data(http.StatusOK, "application/json", raw)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": gin.H{
		"type": "invalid_request_error", "code": "model_not_found", "param": "model",
		"message": fmt.Sprintf("Model %q does not exist or is not available for this group", modelID),
	}})
}
