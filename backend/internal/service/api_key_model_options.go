package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func (s *APIKeyService) GetAvailableGroupModels(ctx context.Context, userID, groupID int64) ([]string, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("get group: %w", err)
	}
	if !group.IsActive() || !s.canUserBindGroup(ctx, user, group) {
		return nil, ErrGroupNotAllowed
	}

	var accounts []Account
	if s.accountRepo != nil {
		accounts, err = s.accountRepo.ListSchedulableByGroupID(ctx, groupID)
		if err != nil {
			return nil, fmt.Errorf("list group model candidates: %w", err)
		}
	}

	models := groupModelOptionsFromAccounts(group, accounts)
	if group.RequestModelMappingEnabled() {
		models = groupRequestModelOptions(group)
	}
	if group.CustomModelsListEnabled() {
		models = filterGroupModelOptions(models, group.ModelsListConfig.Models)
	}
	return models, nil
}

func groupModelOptionsFromAccounts(group *Group, accounts []Account) []string {
	if group == nil {
		return nil
	}
	if group.Platform == PlatformComposite {
		return compositeGroupModelOptions(accounts)
	}

	mappedModels := mappedGroupModelOptions(accounts, group.Platform)
	if len(mappedModels) == 0 {
		return defaultModelsListCandidateIDs(group.Platform)
	}
	if group.Platform == PlatformAnthropic && group.CustomModelsListEnabled() {
		return mergeGroupModelOptions(mappedModels, defaultModelsListCandidateIDs(group.Platform))
	}
	return mappedModels
}

func mappedGroupModelOptions(accounts []Account, platform string) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0)
	for _, account := range accounts {
		if !accountPlatformMatchesGroup(platform, account.Platform) {
			continue
		}
		for model := range account.GetModelMapping() {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			models = append(models, model)
		}
	}
	sort.Strings(models)
	return models
}

func compositeGroupModelOptions(accounts []Account) []string {
	platforms := []string{PlatformAnthropic, PlatformGemini, PlatformOpenAI, PlatformAntigravity, PlatformGrok, PlatformAgnes, PlatformDeepSeek, PlatformNvidia, PlatformTokenRhythm, PlatformKimi, PlatformZhipu, PlatformChatAnywhere, PlatformGLM, PlatformModelScope, PlatformDashScope, PlatformMiniMax, PlatformVolcengine, PlatformSenseNova}
	models := make([]string, 0)
	for _, platform := range platforms {
		platformModels := mappedGroupModelOptions(accounts, platform)
		if len(platformModels) == 0 && hasSchedulableAccountPlatform(accounts, platform) {
			platformModels = defaultModelsListCandidateIDs(platform)
		}
		models = mergeGroupModelOptions(models, platformModels)
	}
	if len(models) == 0 {
		return defaultModelsListCandidateIDs(PlatformComposite)
	}
	return models
}

func hasSchedulableAccountPlatform(accounts []Account, platform string) bool {
	for _, account := range accounts {
		if accountPlatformMatchesGroup(platform, account.Platform) && isConcreteRequestPlatform(account.Platform) {
			return true
		}
	}
	return false
}

func mergeGroupModelOptions(primary, secondary []string) []string {
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	merged := make([]string, 0, len(primary)+len(secondary))
	for _, models := range [][]string{primary, secondary} {
		for _, model := range models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			merged = append(merged, model)
		}
	}
	return merged
}

func groupRequestModelOptions(group *Group) []string {
	if group == nil {
		return nil
	}
	models := make([]string, 0, len(group.ModelsListConfig.ModelMapping))
	for model := range group.ModelsListConfig.ModelMapping {
		model = strings.TrimSpace(model)
		if model != "" {
			models = append(models, model)
		}
	}
	sort.Strings(models)
	return models
}

func filterGroupModelOptions(models, selectedPatterns []string) []string {
	if len(selectedPatterns) == 0 {
		return models
	}

	filtered := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || !groupModelPatternMatches(selectedPatterns, model) {
			continue
		}
		filtered = append(filtered, model)
	}
	return filtered
}

func groupModelPatternMatches(patterns []string, model string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == model {
			return true
		}
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(model, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}
