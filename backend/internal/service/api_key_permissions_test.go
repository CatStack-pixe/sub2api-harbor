//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

type permissionGroupRepo struct {
	GroupRepository
	group *Group
}

func (r *permissionGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	if r.group == nil || r.group.ID != id {
		return nil, ErrGroupNotFound
	}
	cp := *r.group
	return &cp, nil
}

func (r *permissionGroupRepo) ListActive(context.Context) ([]Group, error) {
	if r.group == nil || !r.group.IsActive() {
		return nil, nil
	}
	return []Group{*r.group}, nil
}

type permissionSubRepo struct {
	UserSubscriptionRepository
	sub *UserSubscription
}

func (r *permissionSubRepo) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	if r.sub == nil || r.sub.UserID != userID || r.sub.GroupID != groupID || !r.sub.IsActive() {
		return nil, ErrSubscriptionNotFound
	}
	cp := *r.sub
	return &cp, nil
}

func (r *permissionSubRepo) ListActiveByUserID(ctx context.Context, userID int64) ([]UserSubscription, error) {
	if r.sub == nil {
		return nil, nil
	}
	sub, err := r.GetActiveByUserIDAndGroupID(ctx, userID, r.sub.GroupID)
	if err != nil {
		return nil, nil
	}
	return []UserSubscription{*sub}, nil
}

type permissionKeyRepo struct {
	APIKeyRepository
	key     *APIKey
	creates int
	updates int
	deletes int
}

func (r *permissionKeyRepo) ExistsByKey(context.Context, string) (bool, error) { return false, nil }

func (r *permissionKeyRepo) Create(_ context.Context, key *APIKey) error {
	r.creates++
	cp := *key
	cp.ID = 100
	r.key = &cp
	key.ID = cp.ID
	return nil
}

func (r *permissionKeyRepo) GetByID(_ context.Context, id int64) (*APIKey, error) {
	if r.key == nil || r.key.ID != id {
		return nil, ErrAPIKeyNotFound
	}
	cp := *r.key
	return &cp, nil
}

func (r *permissionKeyRepo) Update(_ context.Context, key *APIKey, fields APIKeyUpdateFields) error {
	r.updates++
	if fields.GroupID {
		r.key.GroupID = key.GroupID
	}
	if fields.Quota {
		r.key.Quota = key.Quota
	}
	if fields.QuotaUsed {
		r.key.QuotaUsed = key.QuotaUsed
	}
	if fields.Status {
		r.key.Status = key.Status
	}
	if fields.Name {
		r.key.Name = key.Name
	}
	return nil
}

func (r *permissionKeyRepo) GetKeyAndOwnerID(ctx context.Context, id int64) (string, int64, error) {
	key, err := r.GetByID(ctx, id)
	if err != nil {
		return "", 0, err
	}
	return key.Key, key.UserID, nil
}

func (r *permissionKeyRepo) DeleteWithAudit(context.Context, int64) error {
	r.deletes++
	return nil
}

func TestAPIKeyService_PermissionMatrix(t *testing.T) {
	const userID, groupID int64 = 42, 7
	now := time.Now()
	for _, tc := range []struct {
		name         string
		restrict     bool
		grants       []int64
		exclusive    bool
		subscription bool
		sub          *UserSubscription
		allow        bool
	}{
		{name: "public self selection", allow: true},
		{name: "public self selection ignores unrelated grants", grants: []int64{99}, allow: true},
		{name: "restricted public grant", restrict: true, grants: []int64{groupID}, allow: true},
		{name: "restricted public no grant", restrict: true},
		{name: "restricted public unrelated grant", restrict: true, grants: []int64{99}},
		{name: "exclusive no grant", exclusive: true},
		{name: "exclusive explicit grant", exclusive: true, grants: []int64{groupID}, allow: true},
		{name: "subscription grant alone is insufficient", subscription: true, grants: []int64{groupID}},
		{name: "subscription expired", subscription: true, sub: &UserSubscription{
			UserID: userID, GroupID: groupID, Status: SubscriptionStatusActive, ExpiresAt: now.Add(-time.Hour),
		}},
		{name: "subscription suspended", subscription: true, sub: &UserSubscription{
			UserID: userID, GroupID: groupID, Status: SubscriptionStatusSuspended, ExpiresAt: now.Add(time.Hour),
		}},
		{name: "subscription belongs to another user", subscription: true, sub: &UserSubscription{
			UserID: 99, GroupID: groupID, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour),
		}},
		{name: "subscription belongs to another group", subscription: true, sub: &UserSubscription{
			UserID: userID, GroupID: 99, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour),
		}},
		{name: "active public subscription", subscription: true, allow: true, sub: &UserSubscription{
			UserID: userID, GroupID: groupID, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour),
		}},
		{name: "active exclusive subscription is its own entitlement", restrict: true, exclusive: true, subscription: true, allow: true, sub: &UserSubscription{
			UserID: userID, GroupID: groupID, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userRepo := &publicGroupsUserRepo{user: &User{
				ID: userID, Status: StatusActive, Balance: 12,
				AllowedGroups: tc.grants, RestrictPublicGroups: tc.restrict,
			}}
			group := &Group{ID: groupID, Status: StatusActive, IsExclusive: tc.exclusive}
			if tc.subscription {
				group.SubscriptionType = SubscriptionTypeSubscription
			} else {
				require.Equal(t, tc.allow, userRepo.user.CanBindGroup(groupID, tc.exclusive))
			}
			keyRepo := &permissionKeyRepo{}
			svc := NewAPIKeyService(keyRepo, userRepo, &permissionGroupRepo{group: group},
				&permissionSubRepo{sub: tc.sub}, nil, nil, &config.Config{})

			available, err := svc.GetAvailableGroups(context.Background(), userID)
			require.NoError(t, err)
			require.Equal(t, tc.allow, len(available) == 1, "bindable group discovery must match writes")
			customKey := "local-permission-key"
			created, err := svc.Create(context.Background(), userID, CreateAPIKeyRequest{
				Name: "local key", CustomKey: &customKey, GroupID: &group.ID, Quota: 7,
			})
			if tc.allow {
				require.NoError(t, err)
				require.Equal(t, groupID, *created.GroupID)
				require.Equal(t, 7.0, created.Quota, "authorized self-service budgets remain supported")
				require.Equal(t, 1, keyRepo.creates)
			} else {
				require.ErrorIs(t, err, ErrGroupNotAllowed)
				require.Nil(t, created)
				require.Zero(t, keyRepo.creates)
			}

			originalGroupID := int64(99)
			keyRepo.key = &APIKey{
				ID: 100, UserID: userID, Key: customKey, GroupID: &originalGroupID,
				Status: StatusActive, Quota: 7, QuotaUsed: 2,
			}
			quota := 19.0
			updated, err := svc.Update(context.Background(), 100, userID, UpdateAPIKeyRequest{
				GroupID: &group.ID, Quota: &quota,
			})
			if tc.allow {
				require.NoError(t, err)
				require.Equal(t, groupID, *updated.GroupID)
				require.Equal(t, quota, keyRepo.key.Quota)
				require.Equal(t, 2.0, keyRepo.key.QuotaUsed)
				require.Equal(t, 1, keyRepo.updates)
			} else {
				require.ErrorIs(t, err, ErrGroupNotAllowed)
				require.Nil(t, updated)
				require.Zero(t, keyRepo.updates)
				require.Equal(t, originalGroupID, *keyRepo.key.GroupID)
				require.Equal(t, 7.0, keyRepo.key.Quota, "a denied rebind must not partially change budget")
			}
			require.Equal(t, 12.0, userRepo.user.Balance)
		})
	}
}

func TestAPIKeyService_PermissionOwnership(t *testing.T) {
	for _, owner := range []bool{false, true} {
		t.Run(map[bool]string{false: "other user", true: "owner"}[owner], func(t *testing.T) {
			repo := &permissionKeyRepo{key: &APIKey{
				ID: 100, UserID: 42, Key: "local-permission-key", Status: StatusActive,
			}}
			svc := NewAPIKeyService(repo, nil, nil, nil, nil, nil, &config.Config{})
			caller := int64(99)
			if owner {
				caller = 42
			}
			name := "renamed"
			_, updateErr := svc.Update(context.Background(), 100, caller, UpdateAPIKeyRequest{Name: &name})
			deleteErr := svc.Delete(context.Background(), 100, caller)
			if owner {
				require.NoError(t, updateErr)
				require.NoError(t, deleteErr)
				require.Equal(t, 1, repo.updates)
				require.Equal(t, 1, repo.deletes)
			} else {
				require.ErrorIs(t, updateErr, ErrInsufficientPerms)
				require.ErrorIs(t, deleteErr, ErrInsufficientPerms)
				require.Zero(t, repo.updates)
				require.Zero(t, repo.deletes)
			}
		})
	}
}

func TestAPIKeyService_RejectsInactiveGroupBinding(t *testing.T) {
	const userID int64 = 42
	userRepo := &publicGroupsUserRepo{user: &User{
		ID: userID, Status: StatusActive, Balance: 12,
	}}
	groupRepo := &permissionGroupRepo{group: &Group{
		ID: 7, Status: StatusDisabled,
	}}
	keyRepo := &permissionKeyRepo{}
	svc := NewAPIKeyService(keyRepo, userRepo, groupRepo, nil, nil, nil, &config.Config{})
	groupID := int64(7)
	customKey := "inactive-group-key"

	created, err := svc.Create(context.Background(), userID, CreateAPIKeyRequest{
		Name: "inactive group", CustomKey: &customKey, GroupID: &groupID,
	})
	require.ErrorIs(t, err, ErrGroupNotAllowed)
	require.Nil(t, created)
	require.Zero(t, keyRepo.creates)
}

func TestAPIKeyService_PermissionUserBudgetRemainsEditable(t *testing.T) {
	for _, quota := range []float64{19, 0} {
		repo := &permissionKeyRepo{key: &APIKey{
			ID: 100, UserID: 42, Key: "local-permission-key", Status: StatusAPIKeyQuotaExhausted,
			Quota: 7, QuotaUsed: 7,
		}}
		userRepo := &publicGroupsUserRepo{user: &User{ID: 42, Balance: 12}}
		svc := NewAPIKeyService(repo, userRepo, nil, nil, nil, nil, &config.Config{})
		key, err := svc.Update(context.Background(), 100, 42, UpdateAPIKeyRequest{Quota: &quota})
		require.NoError(t, err)
		require.Equal(t, quota, key.Quota)
		require.Equal(t, 7.0, key.QuotaUsed)
		require.Equal(t, StatusActive, key.Status)
		require.NoError(t, svc.CheckAPIKeyQuotaAndExpiry(key))
		require.Equal(t, 12.0, userRepo.user.Balance, "editing a key budget must not credit the account")
	}
}

type permissionBillingCache struct {
	BillingCache
	balance float64
	sub     *SubscriptionCacheData
	quota   *UserPlatformQuotaCacheEntry
}

func (c *permissionBillingCache) GetUserBalance(context.Context, int64) (float64, error) {
	return c.balance, nil
}

func (c *permissionBillingCache) GetSubscriptionCache(context.Context, int64, int64) (*SubscriptionCacheData, error) {
	return c.sub, nil
}

func (c *permissionBillingCache) GetUserPlatformQuotaCache(context.Context, int64, string) (*UserPlatformQuotaCacheEntry, bool, error) {
	return c.quota, c.quota != nil, nil
}

func TestAPIKeyService_PermissionBudgetCannotChangeBillingEntitlement(t *testing.T) {
	for _, quota := range []float64{19, 0} {
		repo := &permissionKeyRepo{key: &APIKey{
			ID: 100, UserID: 42, Key: "local-permission-key", Status: StatusAPIKeyQuotaExhausted,
			Quota: 7, QuotaUsed: 7,
		}}
		keys := NewAPIKeyService(repo, nil, nil, nil, nil, nil, &config.Config{})
		key, err := keys.Update(context.Background(), 100, 42, UpdateAPIKeyRequest{Quota: &quota})
		require.NoError(t, err)
		user := &User{ID: 42, Status: StatusActive}
		cache := &permissionBillingCache{}
		billing := &BillingCacheService{cache: cache, cfg: &config.Config{}}
		require.ErrorIs(t, billing.CheckBillingEligibility(context.Background(), user, key, nil, nil, PlatformOpenAI),
			ErrInsufficientBalance, "a larger or unlimited key budget must not grant account credit")

		limit := 1.0
		group := &Group{ID: 7, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit}
		sub := &UserSubscription{UserID: 42, GroupID: 7}
		cache.sub = &SubscriptionCacheData{
			Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour), DailyUsage: limit,
		}
		require.ErrorIs(t, billing.CheckBillingEligibility(context.Background(), user, key, group, sub, PlatformOpenAI),
			ErrDailyLimitExceeded, "editing key quota must not replenish subscription usage")

		cache.balance = 12
		now := time.Now()
		start := timezone.StartOfDay(now)
		weekStart := timezone.StartOfWeek(now)
		cache.quota = &UserPlatformQuotaCacheEntry{
			SchemaVersion: UserPlatformQuotaCacheSchemaV1, DailyLimitUSD: &limit,
			DailyUsageUSD: limit, DailyWindowStart: &start,
			WeeklyWindowStart: &weekStart, MonthlyWindowStart: &now,
		}
		// A cache hit needs no repository access; an unexpected miss panics through
		// the embedded interface instead of silently accepting the request.
		billing.userPlatformQuotaRepo = &struct{ UserPlatformQuotaRepository }{}
		require.ErrorIs(t, billing.CheckBillingEligibility(context.Background(), user, key, nil, nil, PlatformOpenAI),
			ErrUserPlatformDailyQuotaExhausted, "administrator platform limits are independent of key budgets")
	}
}
