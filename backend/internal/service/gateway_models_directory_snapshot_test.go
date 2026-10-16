package service

import (
	"context"
	"sync"
	"testing"
	"time"

	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

func TestAvailableModelDirectoryCachePreservesNilEmptyAndAccountSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name    string
		account Account
		want    []string
		retired bool
	}{
		{
			name:    "nil fallback",
			account: Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		},
		{
			name: "authoritative empty",
			account: Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.5": "gpt-5.5"}}},
			want:    []string{},
			retired: true,
		},
		{
			name: "nonempty catalog",
			account: Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{"model_mapping": map[string]any{"legacy-*": "gpt-6.1-sol"}}},
			want:    []string{"legacy-*"},
			retired: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupID := int64(222)
			repo := &modelsListAccountRepoStub{byGroup: map[int64][]Account{groupID: {tc.account}}}
			svc := &GatewayService{
				accountRepo: repo, modelsListCache: gocache.New(time.Minute, time.Minute),
				modelsListCacheTTL: time.Minute,
			}
			directory := svc.GetAvailableModelDirectory(context.Background(), &groupID, PlatformOpenAI)
			require.Equal(t, tc.want, directory.ModelIDs)
			require.Nil(t, directory.FilterRetiredModels(nil))
			require.NotNil(t, directory.FilterRetiredModels([]string{}))
			// Mutating a returned snapshot must not change either half of the
			// cached directory, including Account's mapping memoization fields.
			directory.ModelIDs = append(directory.ModelIDs, "caller-only")
			directory.ModelIDs[0] = "caller-mutated"
			directory.accounts[0].Type = AccountTypeOAuth
			directory.accounts[0].Credentials = map[string]any{"model_mapping": map[string]any{"gpt-5.5": "gpt-6.1-sol"}}
			repo.byGroup[groupID] = []Account{{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}}
			cached := svc.GetAvailableModelDirectory(context.Background(), &groupID, PlatformOpenAI)
			require.Equal(t, tc.want, cached.ModelIDs)
			require.Equal(t, tc.want, svc.GetAvailableModels(context.Background(), &groupID, PlatformOpenAI))
			require.Equal(t, int64(1), repo.listByGroupCalls.Load())
			wantFiltered := []string{"gpt-5.5"}
			if tc.retired {
				wantFiltered = []string{}
			}
			require.Equal(t, wantFiltered, cached.FilterRetiredModels([]string{"gpt-5.5"}))
			svc.InvalidateAvailableModelsCache(&groupID, PlatformOpenAI)
			refreshed := svc.GetAvailableModelDirectory(context.Background(), &groupID, PlatformOpenAI)
			require.Nil(t, refreshed.ModelIDs)
			require.Equal(t, []string{"gpt-5.5"}, refreshed.FilterRetiredModels([]string{"gpt-5.5"}))
			require.Equal(t, int64(2), repo.listByGroupCalls.Load())
		})
	}
}

func TestAvailableModelDirectoryConcurrentRetirementFiltering(t *testing.T) {
	directory := AvailableModelDirectory{
		platform: PlatformChatAnywhere,
		accounts: []Account{{
			Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Credentials: map[string]any{"model_mapping": map[string]any{
				"legacy-*": "gpt-6.1-sol", "legacy-retired": "gpt-5.5",
			}},
		}},
	}
	var workers sync.WaitGroup
	results := make(chan []string, 16)
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			results <- directory.FilterRetiredModels([]string{"legacy-retired", "legacy-live"})
		}()
	}
	workers.Wait()
	close(results)
	for result := range results {
		require.Equal(t, []string{"legacy-live"}, result)
	}
	require.False(t, directory.accounts[0].modelMappingCacheReady)
}
