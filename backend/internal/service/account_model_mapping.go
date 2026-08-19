package service

import "context"

// AccountModelMappingMerger atomically adds discovered upstream models to an
// account without replacing credentials or existing custom model targets.
type AccountModelMappingMerger interface {
	MergeAccountModelMapping(ctx context.Context, accountID int64, models map[string]string) error
}

type accountModelMappingRepository interface {
	MergeAccountModelMapping(ctx context.Context, accountID int64, models map[string]string) error
}

func (s *adminServiceImpl) MergeAccountModelMapping(ctx context.Context, accountID int64, models map[string]string) error {
	if len(models) == 0 {
		return nil
	}
	if merger, ok := s.accountRepo.(accountModelMappingRepository); ok {
		return merger.MergeAccountModelMapping(ctx, accountID, models)
	}

	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	credentials := mergeAccountModelMappingCredentials(account.Credentials, models)
	return persistAccountCredentials(ctx, s.accountRepo, account, credentials)
}

func mergeAccountModelMappingCredentials(credentials map[string]any, models map[string]string) map[string]any {
	mergedCredentials := make(map[string]any, len(credentials)+1)
	for key, value := range credentials {
		mergedCredentials[key] = value
	}

	mergedMapping := make(map[string]any, len(models))
	for model, target := range models {
		mergedMapping[model] = target
	}
	switch existing := credentials["model_mapping"].(type) {
	case map[string]any:
		for model, target := range existing {
			mergedMapping[model] = target
		}
	case map[string]string:
		for model, target := range existing {
			mergedMapping[model] = target
		}
	}
	mergedCredentials["model_mapping"] = mergedMapping
	return mergedCredentials
}
