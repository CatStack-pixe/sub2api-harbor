package admin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type importedModelSyncAdminService struct {
	*stubAdminService
	account      service.Account
	mergedModels map[string]string
	mergeCalls   int
}

func (s *importedModelSyncAdminService) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	account := s.account
	account.ID = id
	return &account, nil
}

func (s *importedModelSyncAdminService) MergeAccountModelMapping(_ context.Context, _ int64, models map[string]string) error {
	s.mergeCalls++
	s.mergedModels = make(map[string]string, len(models))
	for model, target := range models {
		s.mergedModels[model] = target
	}
	return nil
}

type importedModelSyncHTTPUpstream struct {
	mu        sync.Mutex
	proxyURL  string
	accountID int64
}

func (u *importedModelSyncHTTPUpstream) Do(_ *http.Request, proxyURL string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.proxyURL = proxyURL
	u.accountID = accountID
	u.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"model-a"},{"id":"model-b"},{"id":"model-a"}]}`)),
	}, nil
}

func (u *importedModelSyncHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func newImportedModelSyncHandler(adminSvc service.AdminService, upstream service.HTTPUpstream) *AccountHandler {
	testSvc := service.NewAccountTestService(
		nil,
		nil,
		nil,
		nil,
		nil,
		upstream,
		&config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		nil,
	)
	return NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, testSvc, nil, nil, nil, nil, nil)
}

func TestSyncImportedAccountModelsUsesHydratedProxyAndPersistsModels(t *testing.T) {
	proxyID := int64(19)
	adminSvc := &importedModelSyncAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       300,
			Name:     "imported",
			Platform: service.PlatformChatAnywhere,
			Type:     service.AccountTypeAPIKey,
			Credentials: map[string]any{
				"api_key":  "test-key",
				"base_url": "https://api.chatanywhere.tech/v1",
			},
			ProxyID: &proxyID,
			Proxy: &service.Proxy{
				ID:       proxyID,
				Protocol: "http",
				Host:     "127.0.0.1",
				Port:     8080,
			},
		},
	}
	upstream := &importedModelSyncHTTPUpstream{}
	handler := newImportedModelSyncHandler(adminSvc, upstream)

	outcome := handler.syncImportedAccountModels(context.Background(), &service.Account{ID: 300, Name: "imported"})

	require.Equal(t, importedModelSyncSucceeded, outcome.Status)
	require.Equal(t, 2, outcome.Count)
	require.Equal(t, map[string]string{"model-a": "model-a", "model-b": "model-b"}, adminSvc.mergedModels)
	require.Equal(t, 1, adminSvc.mergeCalls)
	require.Equal(t, int64(300), upstream.accountID)
	require.NotEmpty(t, upstream.proxyURL)
}

func TestNormalizeImportedUpstreamModelsRejectsUnsafeLists(t *testing.T) {
	models := make([]string, maxImportedUpstreamModels+1)
	for index := range models {
		models[index] = fmt.Sprintf("model-%03d", index)
	}
	_, err := normalizeImportedUpstreamModels(models)
	require.ErrorContains(t, err, "more than")

	_, err = normalizeImportedUpstreamModels([]string{strings.Repeat("m", maxImportedUpstreamModelIDSize+1)})
	require.ErrorContains(t, err, "exceeds")

	_, err = normalizeImportedUpstreamModels([]string{"", "  "})
	require.ErrorContains(t, err, "no supported models")
}

func TestMergeImportedModelMappingCredentialsPreservesCustomTargets(t *testing.T) {
	credentials := map[string]any{
		"api_key": "secret",
		"model_mapping": map[string]any{
			"model-a":      "custom-target",
			"legacy-model": "legacy-target",
		},
	}

	merged := mergeImportedModelMappingCredentials(credentials, map[string]string{
		"model-a": "model-a",
		"model-b": "model-b",
	})
	mapping, ok := merged["model_mapping"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "custom-target", mapping["model-a"])
	require.Equal(t, "model-b", mapping["model-b"])
	require.Equal(t, "legacy-target", mapping["legacy-model"])
	require.Equal(t, "secret", merged["api_key"])
}

func TestImportDataAutomaticallySyncsCreatedAccounts(t *testing.T) {
	adminSvc := &importedModelSyncAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       300,
			Name:     "chatanywhere-import",
			Platform: service.PlatformChatAnywhere,
			Type:     service.AccountTypeAPIKey,
			Credentials: map[string]any{
				"api_key":  "test-key",
				"base_url": "https://api.chatanywhere.tech/v1",
			},
		},
	}
	handler := newImportedModelSyncHandler(adminSvc, &importedModelSyncHTTPUpstream{})

	result, err := handler.importData(context.Background(), DataImportRequest{Data: DataPayload{
		Accounts: []DataAccount{{
			Name:        "chatanywhere-import",
			Platform:    service.PlatformChatAnywhere,
			Type:        service.AccountTypeAPIKey,
			Credentials: map[string]any{"api_key": "test-key", "base_url": "https://api.chatanywhere.tech/v1"},
			Concurrency: 1,
			Priority:    1,
		}},
	}})

	require.NoError(t, err)
	require.Equal(t, 1, result.AccountCreated)
	require.Equal(t, 1, result.ModelSyncSucceeded)
	require.Zero(t, result.ModelSyncSkipped)
	require.Zero(t, result.ModelSyncFailed)
	require.Equal(t, 1, adminSvc.mergeCalls)
}
