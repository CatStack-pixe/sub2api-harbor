package service

import (
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// testTierflowAccountConnection uses the same Chat Completions transport as the
// gateway. A relay's model catalog is account-specific: an empty selection must
// not silently send a paid probe to an invented OpenAI default model.
func (s *AccountTestService) testTierflowAccountConnection(c *gin.Context, account *Account, modelID, prompt string) error {
	modelID = tierflowAccountTestModel(account, modelID)
	if modelID == "" {
		return s.sendErrorAndEnd(c, "Select a Tierflow model or sync the upstream model list before testing")
	}
	modelID = account.GetMappedModel(modelID)
	authToken := strings.TrimSpace(account.GetOpenAIProtocolAPIKey())
	if authToken == "" {
		return s.sendErrorAndEnd(c, "No API key available")
	}
	baseURL, err := s.validateUpstreamBaseURL(account.GetOpenAIBaseURL())
	if err != nil {
		return s.sendErrorAndEnd(c, "Invalid Tierflow base URL")
	}
	return s.testOpenAIChatCompletionsConnection(c, account, modelID, prompt, baseURL, authToken)
}

func tierflowAccountTestModel(account *Account, modelID string) string {
	if modelID = strings.TrimSpace(modelID); modelID != "" {
		return modelID
	}
	if account == nil {
		return ""
	}
	models := make([]string, 0)
	for publicModel, upstreamModel := range account.GetModelMapping() {
		if strings.TrimSpace(publicModel) == "" || strings.TrimSpace(upstreamModel) == "" ||
			strings.ContainsAny(publicModel, "*?") || strings.ContainsAny(upstreamModel, "*?") {
			continue
		}
		models = append(models, publicModel)
	}
	if len(models) == 0 {
		return ""
	}
	sort.Strings(models)
	return models[0]
}
