//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type updateAccountCredsRepoStub struct {
	mockAccountRepoForGemini
	account     *Account
	created     *Account
	updateCalls int
}

func (r *updateAccountCredsRepoStub) Create(_ context.Context, account *Account) error {
	r.created = account
	return nil
}

func (r *updateAccountCredsRepoStub) GetByID(ctx context.Context, id int64) (*Account, error) {
	return r.account, nil
}

func (r *updateAccountCredsRepoStub) Update(ctx context.Context, account *Account) error {
	r.updateCalls++
	r.account = account
	return nil
}

func TestUpdateAccount_PreservesSensitiveCredsWhenIncomingOmits(t *testing.T) {
	accountID := int64(202)
	repo := &updateAccountCredsRepoStub{
		account: &Account{
			ID:       accountID,
			Platform: PlatformAnthropic,
			Type:     AccountTypeOAuth,
			Status:   StatusActive,
			Credentials: map[string]any{
				"refresh_token": "rt-existing",
				"access_token":  "at-existing",
				"id_token":      "id-existing",
				"base_url":      "https://old.example.com",
			},
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	// 模拟前端编辑：仅修改 base_url，没有传 token（脱敏后前端 spread 拿不到敏感键）
	updated, err := svc.UpdateAccount(context.Background(), accountID, &UpdateAccountInput{
		Credentials: map[string]any{
			"base_url": "https://new.example.com",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 1, repo.updateCalls)

	// 敏感键应保留
	require.Equal(t, "rt-existing", repo.account.Credentials["refresh_token"])
	require.Equal(t, "at-existing", repo.account.Credentials["access_token"])
	require.Equal(t, "id-existing", repo.account.Credentials["id_token"])
	// 非敏感键被替换
	require.Equal(t, "https://new.example.com", repo.account.Credentials["base_url"])
}

func TestUpdateAccount_ExplicitNewTokenOverwrites(t *testing.T) {
	accountID := int64(203)
	repo := &updateAccountCredsRepoStub{
		account: &Account{
			ID:       accountID,
			Platform: PlatformAnthropic,
			Type:     AccountTypeOAuth,
			Status:   StatusActive,
			Credentials: map[string]any{
				"refresh_token": "rt-old",
				"api_key":       "sk-old",
			},
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	updated, err := svc.UpdateAccount(context.Background(), accountID, &UpdateAccountInput{
		Credentials: map[string]any{
			"refresh_token": "rt-new",
			// api_key 没传 → 应保留旧值
		},
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	require.Equal(t, "rt-new", repo.account.Credentials["refresh_token"])
	require.Equal(t, "sk-old", repo.account.Credentials["api_key"])
}

func TestUpdateAccount_EmptyCredentialsSkipsUpdate(t *testing.T) {
	accountID := int64(204)
	repo := &updateAccountCredsRepoStub{
		account: &Account{
			ID:       accountID,
			Platform: PlatformAnthropic,
			Type:     AccountTypeOAuth,
			Status:   StatusActive,
			Credentials: map[string]any{
				"refresh_token": "rt-existing",
			},
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.UpdateAccount(context.Background(), accountID, &UpdateAccountInput{
		Credentials: map[string]any{}, // len == 0 → 闸门跳过
		Name:        "renamed",
	})
	require.NoError(t, err)

	require.Equal(t, "rt-existing", repo.account.Credentials["refresh_token"], "空 credentials 不应触碰已有 token")
	require.Equal(t, "renamed", repo.account.Name)
}

func TestCreateAccount_StripsClientManagedGrokTier(t *testing.T) {
	tests := []struct {
		name            string
		trusted         bool
		wantTier        string
		wantEntitlement string
	}{
		{name: "generic admin input", wantTier: "", wantEntitlement: ""},
		{name: "provider OAuth input", trusted: true, wantTier: "supergrok_heavy", wantEntitlement: "active"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &updateAccountCredsRepoStub{}
			svc := &adminServiceImpl{accountRepo: repo}

			created, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
				Name:     "grok-oauth",
				Platform: PlatformGrok,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"access_token":       "token",
					"subscription_tier":  "supergrok_heavy",
					"entitlement_status": "active",
				},
				SkipDefaultGroupBind:       true,
				ProviderManagedCredentials: tt.trusted,
			})

			require.NoError(t, err)
			require.NotNil(t, created)
			require.NotNil(t, repo.created)
			require.Equal(t, tt.wantTier, repo.created.GetCredential("subscription_tier"))
			require.Equal(t, tt.wantEntitlement, repo.created.GetCredential("entitlement_status"))
		})
	}
}

func TestUpdateAccount_OnlyProviderFlowCanChangeGrokTier(t *testing.T) {
	tests := []struct {
		name            string
		trusted         bool
		wantTier        string
		wantEntitlement string
	}{
		{name: "generic admin input preserves stored provider state", wantTier: "free", wantEntitlement: "inactive"},
		{name: "provider OAuth input replaces provider state", trusted: true, wantTier: "supergrok_heavy", wantEntitlement: "active"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &updateAccountCredsRepoStub{account: &Account{
				ID:       205,
				Platform: PlatformGrok,
				Type:     AccountTypeOAuth,
				Status:   StatusActive,
				Credentials: map[string]any{
					"access_token":       "old-token",
					"subscription_tier":  "free",
					"entitlement_status": "inactive",
				},
			}}
			svc := &adminServiceImpl{accountRepo: repo}

			updated, err := svc.UpdateAccount(context.Background(), 205, &UpdateAccountInput{
				Credentials: map[string]any{
					"access_token":       "new-token",
					"subscription_tier":  "supergrok_heavy",
					"entitlement_status": "active",
				},
				ProviderManagedCredentials: tt.trusted,
			})

			require.NoError(t, err)
			require.NotNil(t, updated)
			require.Equal(t, tt.wantTier, updated.GetCredential("subscription_tier"))
			require.Equal(t, tt.wantEntitlement, updated.GetCredential("entitlement_status"))
			require.Equal(t, "new-token", updated.GetCredential("access_token"))
		})
	}
}
