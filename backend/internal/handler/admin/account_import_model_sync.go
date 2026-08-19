package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	importedModelSyncWorkers       = 8
	importedModelSyncTimeout       = 30 * time.Second
	maxImportedUpstreamModels      = 512
	maxImportedUpstreamModelIDSize = 256
)

type importedModelSyncStatus string

const (
	importedModelSyncSucceeded importedModelSyncStatus = "success"
	importedModelSyncSkipped   importedModelSyncStatus = "skipped"
	importedModelSyncFailed    importedModelSyncStatus = "failed"
)

type importedModelSyncOutcome struct {
	AccountID int64
	Name      string
	Status    importedModelSyncStatus
	Count     int
	Err       error
}

func (h *AccountHandler) syncImportedAccounts(ctx context.Context, accounts []*service.Account) []importedModelSyncOutcome {
	outcomes := make([]importedModelSyncOutcome, len(accounts))
	if len(accounts) == 0 {
		return outcomes
	}

	workers := min(importedModelSyncWorkers, len(accounts))
	jobs := make(chan int)
	var wg sync.WaitGroup
	baseCtx := context.WithoutCancel(ctx)

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				syncCtx, cancel := context.WithTimeout(baseCtx, importedModelSyncTimeout)
				outcomes[index] = h.syncImportedAccountModels(syncCtx, accounts[index])
				cancel()
			}
		}()
	}

	for index := range accounts {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	return outcomes
}

func (h *AccountHandler) syncImportedAccountModels(ctx context.Context, account *service.Account) importedModelSyncOutcome {
	outcome := importedModelSyncOutcome{Status: importedModelSyncFailed}
	if account == nil {
		outcome.Err = errors.New("imported account is nil")
		return outcome
	}
	outcome.AccountID = account.ID
	outcome.Name = account.Name

	if h.accountTestService == nil {
		outcome.Status = importedModelSyncSkipped
		return outcome
	}

	// CreateAccount does not hydrate Proxy. Reloading guarantees model discovery
	// uses the proxy that was assigned to the imported account.
	latest, err := h.adminService.GetAccount(ctx, account.ID)
	if err != nil {
		outcome.Err = err
		return outcome
	}
	if latest == nil {
		outcome.Err = errors.New("imported account was not found")
		return outcome
	}
	if strings.TrimSpace(latest.Name) != "" {
		outcome.Name = latest.Name
	}

	models, err := h.accountTestService.FetchUpstreamSupportedModels(ctx, latest)
	if err != nil {
		var syncErr *service.UpstreamModelSyncError
		if errors.As(err, &syncErr) && syncErr.Kind == service.UpstreamModelSyncErrorUnsupported {
			outcome.Status = importedModelSyncSkipped
			return outcome
		}
		outcome.Err = err
		return outcome
	}

	modelMapping, err := normalizeImportedUpstreamModels(models)
	if err != nil {
		outcome.Err = err
		return outcome
	}

	if merger, ok := h.adminService.(service.AccountModelMappingMerger); ok {
		err = merger.MergeAccountModelMapping(ctx, latest.ID, modelMapping)
	} else {
		// Narrow test doubles and third-party AdminService implementations may not
		// expose the atomic merger. Reload once more to minimize stale overwrites.
		fresh, getErr := h.adminService.GetAccount(ctx, latest.ID)
		if getErr != nil {
			outcome.Err = getErr
			return outcome
		}
		if fresh == nil {
			outcome.Err = errors.New("imported account was not found")
			return outcome
		}
		credentials := mergeImportedModelMappingCredentials(fresh.Credentials, modelMapping)
		_, err = h.adminService.UpdateAccount(ctx, latest.ID, &service.UpdateAccountInput{
			Credentials:           credentials,
			SkipMixedChannelCheck: true,
		})
	}
	if err != nil {
		outcome.Err = err
		return outcome
	}

	outcome.Status = importedModelSyncSucceeded
	outcome.Count = len(modelMapping)
	return outcome
}

func normalizeImportedUpstreamModels(models []string) (map[string]string, error) {
	mapping := make(map[string]string, len(models))
	for _, rawModel := range models {
		model := strings.TrimSpace(rawModel)
		if model == "" {
			continue
		}
		if len(model) > maxImportedUpstreamModelIDSize {
			return nil, fmt.Errorf("upstream model ID exceeds %d bytes", maxImportedUpstreamModelIDSize)
		}
		mapping[model] = model
		if len(mapping) > maxImportedUpstreamModels {
			return nil, fmt.Errorf("upstream returned more than %d models", maxImportedUpstreamModels)
		}
	}
	if len(mapping) == 0 {
		return nil, errors.New("upstream returned no supported models")
	}
	return mapping, nil
}

func mergeImportedModelMappingCredentials(credentials map[string]any, models map[string]string) map[string]any {
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

func importedModelSyncClientMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Timed out syncing upstream models"
	}
	var syncErr *service.UpstreamModelSyncError
	if errors.As(err, &syncErr) {
		return syncErr.SafeMessage()
	}
	return "Failed to persist synchronized upstream models"
}

func logImportedModelSyncFailure(outcome importedModelSyncOutcome) {
	kind := "internal"
	var syncErr *service.UpstreamModelSyncError
	if errors.As(outcome.Err, &syncErr) {
		kind = string(syncErr.Kind)
	} else if errors.Is(outcome.Err, context.DeadlineExceeded) {
		kind = "timeout"
	}
	slog.Warn("imported_account_model_sync_failed", "account_id", outcome.AccountID, "kind", kind)
}
