//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestFilterBatchImageModelsByAPIKeyWhitelist(t *testing.T) {
	models := []service.BatchImagePublicModel{
		{ID: "gemini-2.5-flash-image", Object: "image.batch.model", Provider: service.BatchImageProviderGeminiAPI},
		{ID: "gemini-3.1-flash-image", Object: "image.batch.model", Provider: service.BatchImageProviderVertex},
	}
	apiKey := &service.APIKey{ModelWhitelist: []string{"gemini-2.5-flash-image"}}

	filtered := filterBatchImageModels(apiKey, models)
	require.Equal(t, []service.BatchImagePublicModel{models[0]}, filtered)
}
