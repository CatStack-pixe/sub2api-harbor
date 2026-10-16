package service

import (
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// AvailableModelDirectory keeps listing candidates and their account pool in
// one snapshot, including when candidates are served from the short cache.
type AvailableModelDirectory struct {
	ModelIDs []string
	accounts []Account
	platform string
}

func (d AvailableModelDirectory) clone() AvailableModelDirectory {
	if d.ModelIDs != nil {
		models := make([]string, len(d.ModelIDs))
		copy(models, d.ModelIDs)
		d.ModelIDs = models
	}
	d.accounts = append([]Account(nil), d.accounts...)
	return d
}

// FilterRetiredModels must also run after custom-list or allowlist expansion:
// a surviving wildcard can otherwise reintroduce a retired exact mapping.
func (d AvailableModelDirectory) FilterRetiredModels(models []string) []string {
	return d.FilterRetiredModelsForGroup(models, nil)
}

// FilterRetiredModelsForGroup resolves group aliases before account mappings,
// while retaining the public names used by the catalog and API-key permissions.
func (d AvailableModelDirectory) FilterRetiredModelsForGroup(models []string, group *Group) []string {
	if !domain.UsesOpenAIGateway(d.platform) {
		return models
	}
	// Account model lookups memoize mappings. Keep those writes local to this
	// invocation rather than mutating an account slice shared with the cache.
	accounts := append([]Account(nil), d.accounts...)
	return filterRetiredOpenAIModelIDsForGroup(accounts, models, group)
}

// GPT-5.5 retired from Codex with ChatGPT sign-in on 2026-10-14 at
// 19:00 UTC. API-key upstreams remain supported. Do not infer upstream
// eligibility from the API key a client uses to authenticate to this gateway.
// https://learn.chatgpt.com/docs/models#gpt-55-retirement
func isRetiredOpenAISubscriptionModel(account *Account, upstreamModel string) bool {
	if account == nil || !account.IsOpenAI() || (!account.IsOpenAIOAuthLike() && !account.IsShadow()) {
		return false
	}
	model := canonicalizeOpenAIModelAliasSpelling(upstreamModel)
	return model == "gpt-5.5" || strings.HasPrefix(model, "gpt-5.5-")
}

// Only remove entries blocked by retirement. Unknown models and other platforms
// retain their existing listing policy. An explicitly mapped replacement is
// allowed, but a public alias targeting retired GPT-5.5 is not.
func openAIModelRetiredForAccounts(accounts []Account, modelID string) bool {
	retired := false
	for i := range accounts {
		account := &accounts[i]
		// A compatible provider can still serve a name retired by ChatGPT.
		// Keep its support check aligned with the shared OpenAI account pool.
		if !account.IsOpenAICompatible() {
			continue
		}
		target := resolveOpenAIAccountUpstreamModelForRequest(account, modelID, false)
		if isRetiredOpenAISubscriptionModel(account, target) {
			retired = true
		} else if account.IsModelSupported(modelID) {
			return false
		}
	}
	return retired
}

func filterRetiredOpenAIModelIDs(accounts []Account, models []string) []string {
	return filterRetiredOpenAIModelIDsForGroup(accounts, models, nil)
}

func filterRetiredOpenAIModelIDsForGroup(accounts []Account, models []string, group *Group) []string {
	if models == nil {
		return nil
	}
	filtered := make([]string, 0, len(models))
	for _, model := range models {
		requestedModel, _ := group.ResolveRequestModel(model)
		if !openAIModelRetiredForAccounts(accounts, requestedModel) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

// Entries must already use client-visible names, not raw upstream IDs. Resolve
// each public name exactly as routing does, after projection and before the
// final client ETag is calculated. Preserve unknown fields.
func filterRetiredOpenAIPublicModelsBody(body []byte, accounts []Account, codex bool) ([]byte, bool, error) {
	field, idField := "data", "id"
	if codex {
		field, idField = "models", "slug"
	}
	envelope, entries, err := modelCatalogEntries(body, field)
	if err != nil {
		return nil, false, err
	}
	filtered := make([]json.RawMessage, 0, len(entries))
	for _, raw := range entries {
		var entry map[string]json.RawMessage
		var id string
		if json.Unmarshal(raw, &entry) == nil {
			_ = json.Unmarshal(entry[idField], &id)
		}
		if !openAIModelRetiredForAccounts(accounts, id) {
			filtered = append(filtered, raw)
		}
	}
	if len(filtered) == len(entries) {
		return body, false, nil
	}
	envelope[field], err = json.Marshal(filtered)
	if err != nil {
		return nil, false, err
	}
	encoded, err := json.Marshal(envelope)
	return encoded, true, err
}

// Compact-only administrator mappings are evaluated against their actual
// target, without treating an old client-visible name as a retired target.
func supportsOpenAIRequestModel(account *Account, requestedModel string, requireCompact bool) bool {
	if isRetiredOpenAISubscriptionModel(account, resolveOpenAIAccountUpstreamModelForRequest(account, requestedModel, requireCompact)) {
		return false
	}
	return account.isModelMappingSupported(requestedModel)
}
