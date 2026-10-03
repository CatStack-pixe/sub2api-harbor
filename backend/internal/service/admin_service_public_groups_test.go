//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type publicGroupsUserRepo struct {
	UserRepository
	user      *User
	fields    []UserUpdateFields
	updateErr error
}

func (r *publicGroupsUserRepo) GetByID(_ context.Context, id int64) (*User, error) {
	if r.user == nil || r.user.ID != id {
		return nil, ErrUserNotFound
	}
	cp := *r.user
	cp.AllowedGroups = append([]int64(nil), r.user.AllowedGroups...)
	return &cp, nil
}

func (r *publicGroupsUserRepo) Update(_ context.Context, user *User, fields UserUpdateFields) error {
	r.fields = append(r.fields, fields)
	if r.updateErr != nil {
		return r.updateErr
	}
	if fields.RestrictPublicGroups {
		r.user.RestrictPublicGroups = user.RestrictPublicGroups
	}
	if fields.AllowedGroups {
		r.user.AllowedGroups = append([]int64(nil), user.AllowedGroups...)
	}
	return nil
}

func TestAdminService_UpdateUser_PublicGroupRestriction(t *testing.T) {
	enabled, disabled := true, false
	for _, tc := range []struct {
		name       string
		before     bool
		input      *bool
		want       bool
		invalidate bool
	}{
		{name: "enable", input: &enabled, want: true, invalidate: true},
		{name: "disable", before: true, input: &disabled, invalidate: true},
		{name: "omitted preserves enabled", before: true, want: true},
		{name: "omitted preserves disabled"},
		{name: "same enabled", before: true, input: &enabled, want: true},
		{name: "same disabled", input: &disabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &publicGroupsUserRepo{user: &User{
				ID: 42, Role: RoleUser, Status: StatusActive, Balance: 12,
				AllowedGroups: []int64{7}, RestrictPublicGroups: tc.before,
			}}
			invalidator := &authCacheInvalidatorStub{}
			svc := &adminServiceImpl{userRepo: repo, authCacheInvalidator: invalidator}

			got, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{
				RestrictPublicGroups: tc.input,
			})
			require.NoError(t, err)
			require.Equal(t, tc.want, got.RestrictPublicGroups)
			require.Equal(t, tc.want, repo.user.RestrictPublicGroups)
			require.Equal(t, []int64{7}, repo.user.AllowedGroups)
			require.Equal(t, 12.0, repo.user.Balance)
			require.Len(t, repo.fields, 1)
			require.Equal(t, UserUpdateFields{RestrictPublicGroups: tc.input != nil}, repo.fields[0],
				"only the explicitly supplied permission column may be written")
			if tc.invalidate {
				require.Equal(t, []int64{42}, invalidator.userIDs)
			} else {
				require.Empty(t, invalidator.userIDs)
			}
		})
	}
}

func TestAdminService_UpdateUser_PublicGroupRestrictionWriteFailure(t *testing.T) {
	failure := errors.New("local mock persistence failure")
	repo := &publicGroupsUserRepo{
		user:      &User{ID: 42, RestrictPublicGroups: false},
		updateErr: failure,
	}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{userRepo: repo, authCacheInvalidator: invalidator}
	enabled := true

	got, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{
		RestrictPublicGroups: &enabled,
	})
	require.ErrorIs(t, err, failure)
	require.Nil(t, got)
	require.False(t, repo.user.RestrictPublicGroups, "a failed write must not change stored permissions")
	require.Empty(t, invalidator.userIDs, "invalidation belongs after a successful write")
}

func TestAdminService_UpdateUser_PublicGroupRestrictionAndGrants(t *testing.T) {
	repo := &publicGroupsUserRepo{user: &User{ID: 42, AllowedGroups: []int64{7}}}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{userRepo: repo, authCacheInvalidator: invalidator}
	enabled := true
	grants := []int64{9}

	_, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{
		RestrictPublicGroups: &enabled,
		AllowedGroups:        &grants,
	})
	require.NoError(t, err)
	require.True(t, repo.user.RestrictPublicGroups)
	require.Equal(t, grants, repo.user.AllowedGroups)
	require.Equal(t, []UserUpdateFields{{AllowedGroups: true, RestrictPublicGroups: true}}, repo.fields)
	require.Equal(t, []int64{42}, invalidator.userIDs, "invalidate all user keys once after both permissions persist")
}

type publicGroupsAuthCache struct {
	authCacheStub
	entries   map[string]*APIKeyAuthCacheEntry
	publishes int
}

func (c *publicGroupsAuthCache) GetAuthCache(_ context.Context, key string) (*APIKeyAuthCacheEntry, error) {
	return c.entries[key], nil
}

func (c *publicGroupsAuthCache) SetAuthCache(_ context.Context, key string, entry *APIKeyAuthCacheEntry, _ time.Duration) error {
	c.entries[key] = entry
	return nil
}

func (c *publicGroupsAuthCache) DeleteAuthCache(_ context.Context, key string) error {
	delete(c.entries, key)
	return nil
}

func (c *publicGroupsAuthCache) PublishAuthCacheInvalidation(context.Context, string) error {
	c.publishes++
	return nil
}

func TestAdminService_UpdateUser_PublicGroupRestrictionInvalidatesL1L2(t *testing.T) {
	for _, exclusive := range []bool{false, true} {
		t.Run(map[bool]string{false: "restrict public", true: "revoke exclusive"}[exclusive], func(t *testing.T) {
			users := &publicGroupsUserRepo{user: &User{ID: 42, Status: StatusActive, Balance: 12}}
			if exclusive {
				users.user.AllowedGroups = []int64{7}
			}
			group := &Group{ID: 7, Status: StatusActive, IsExclusive: exclusive, Hydrated: true}
			keys := []string{"local-first-key", "local-second-key"}
			loads := 0
			repo := &authRepoStub{
				getByKeyForAuth: func(ctx context.Context, key string) (*APIKey, error) {
					loads++
					user, err := users.GetByID(ctx, 42)
					if err != nil {
						return nil, err
					}
					return &APIKey{
						ID: 100, UserID: 42, Key: key, Status: StatusActive,
						GroupID: &group.ID, Group: group, User: user,
					}, nil
				},
				listKeysByUserID: func(_ context.Context, userID int64) ([]string, error) {
					require.Equal(t, int64(42), userID)
					return keys, nil
				},
			}
			cache := &publicGroupsAuthCache{entries: make(map[string]*APIKeyAuthCacheEntry)}
			cfg := &config.Config{APIKeyAuth: config.APIKeyAuthCacheConfig{
				L1Size: 1024, L1TTLSeconds: 60, L2TTLSeconds: 60,
			}}
			apiKeys := NewAPIKeyService(repo, users, nil, nil, nil, cache, cfg)
			require.NotNil(t, apiKeys.authCacheL1)
			t.Cleanup(apiKeys.authCacheL1.Close)
			for _, key := range keys {
				_, err := apiKeys.GetByKey(context.Background(), key)
				require.NoError(t, err)
			}
			apiKeys.authCacheL1.Wait()
			require.Len(t, cache.entries, 2)
			for _, key := range keys {
				_, cached := apiKeys.authCacheL1.Get(apiKeys.authCacheKey(key))
				require.True(t, cached, "warm the actual L1 before revoking permission")
			}

			input := &UpdateUserInput{}
			if exclusive {
				empty := []int64{}
				input.AllowedGroups = &empty
			} else {
				enabled := true
				input.RestrictPublicGroups = &enabled
			}
			admin := &adminServiceImpl{userRepo: users, authCacheInvalidator: apiKeys}
			_, err := admin.UpdateUser(context.Background(), 42, input)
			require.NoError(t, err)
			apiKeys.authCacheL1.Wait()
			require.Empty(t, cache.entries)
			require.Equal(t, 2, cache.publishes, "publish revocation for every user key")
			for _, key := range keys {
				_, cached := apiKeys.authCacheL1.Get(apiKeys.authCacheKey(key))
				require.False(t, cached)
				fresh, err := apiKeys.GetByKey(context.Background(), key)
				require.NoError(t, err)
				require.False(t, fresh.User.CanBindGroup(group.ID, group.IsExclusive))
			}
			require.Equal(t, 4, loads, "both revoked keys must reload current permissions")
		})
	}
}
