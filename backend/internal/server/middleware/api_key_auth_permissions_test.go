//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func permissionAuthRouter(apiKeys *service.APIKeyService, subscriptions *service.SubscriptionService, cfg *config.Config, google bool, calls *int) *gin.Engine {
	router := gin.New()
	if google {
		router.Use(APIKeyAuthWithSubscriptionGoogle(apiKeys, subscriptions, cfg))
	} else {
		router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiKeys, subscriptions, cfg)))
	}
	router.GET("/permission", func(c *gin.Context) {
		(*calls)++
		c.Status(http.StatusOK)
	})
	return router
}

func permissionAuthRequest(router *gin.Engine) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/permission", nil)
	request.Header.Set("x-api-key", "local-permission-key")
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestAPIKeyAuthPermissionMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, google := range []bool{false, true} {
		protocol := map[bool]string{false: "standard", true: "google"}[google]
		t.Run(protocol, func(t *testing.T) {
			for _, tc := range []struct {
				name         string
				restrict     bool
				grants       []int64
				exclusive    bool
				subscription bool
				subStatus    string
				subExpired   bool
				wantStatus   int
			}{
				{name: "public self selection", wantStatus: http.StatusOK},
				{name: "unrestricted public with unrelated grants", grants: []int64{99}, wantStatus: http.StatusOK},
				{name: "restricted public granted", restrict: true, grants: []int64{7}, wantStatus: http.StatusOK},
				{name: "restricted public unauthorized", restrict: true, wantStatus: http.StatusForbidden},
				{name: "exclusive unauthorized", exclusive: true, wantStatus: http.StatusForbidden},
				{name: "exclusive granted", exclusive: true, grants: []int64{7}, wantStatus: http.StatusOK},
				{name: "subscription absent despite grant", subscription: true, grants: []int64{7}, wantStatus: http.StatusForbidden},
				{name: "subscription expired", subscription: true, subStatus: service.SubscriptionStatusActive, subExpired: true, wantStatus: http.StatusForbidden},
				{name: "subscription suspended", subscription: true, subStatus: service.SubscriptionStatusSuspended, wantStatus: http.StatusForbidden},
				{name: "active public subscription", subscription: true, subStatus: service.SubscriptionStatusActive, wantStatus: http.StatusOK},
				{name: "active exclusive subscription is its own entitlement", restrict: true, exclusive: true, subscription: true, subStatus: service.SubscriptionStatusActive, wantStatus: http.StatusOK},
			} {
				t.Run(tc.name, func(t *testing.T) {
					user := &service.User{
						ID: 42, Role: service.RoleUser, Status: service.StatusActive, Balance: 12,
						AllowedGroups: tc.grants, RestrictPublicGroups: tc.restrict,
					}
					group := &service.Group{ID: 7, Status: service.StatusActive, IsExclusive: tc.exclusive, Hydrated: true}
					if tc.subscription {
						group.SubscriptionType = service.SubscriptionTypeSubscription
					}
					repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) {
						return &service.APIKey{
							ID: 100, UserID: user.ID, Key: "local-permission-key", Status: service.StatusActive,
							GroupID: &group.ID, Group: group, User: user,
						}, nil
					}}
					subRepo := &stubUserSubscriptionRepo{getActive: func(_ context.Context, userID, groupID int64) (*service.UserSubscription, error) {
						if tc.subStatus != service.SubscriptionStatusActive || tc.subExpired {
							return nil, service.ErrSubscriptionNotFound
						}
						return &service.UserSubscription{
							ID: 200, UserID: userID, GroupID: groupID, Status: tc.subStatus,
							ExpiresAt: time.Now().Add(time.Hour), DailyWindowStart: ptrToPermissionTime(time.Now()),
						}, nil
					}}
					cfg := &config.Config{RunMode: config.RunModeStandard}
					apiKeys := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
					subscriptions := service.NewSubscriptionService(nil, subRepo, nil, nil, cfg)
					t.Cleanup(subscriptions.Stop)
					calls := 0
					recorder := permissionAuthRequest(permissionAuthRouter(apiKeys, subscriptions, cfg, google, &calls))
					require.Equal(t, tc.wantStatus, recorder.Code, recorder.Body.String())
					if tc.wantStatus == http.StatusOK {
						require.Equal(t, 1, calls)
					} else {
						require.Zero(t, calls, "denied requests must stop before any downstream work")
					}
				})
			}
		})
	}
}

func TestAPIKeyAuthSimpleModeRequiresSubscriptionEntitlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, google := range []bool{false, true} {
		protocol := map[bool]string{false: "standard", true: "google"}[google]
		t.Run(protocol, func(t *testing.T) {
			for _, active := range []bool{false, true} {
				name := map[bool]string{false: "missing subscription", true: "active subscription"}[active]
				t.Run(name, func(t *testing.T) {
					user := &service.User{
						ID: 42, Role: service.RoleUser, Status: service.StatusActive, Balance: 12,
					}
					group := &service.Group{
						ID: 7, Status: service.StatusActive,
						SubscriptionType: service.SubscriptionTypeSubscription, Hydrated: true,
					}
					keys := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) {
						return &service.APIKey{
							ID: 100, UserID: user.ID, Key: "local-permission-key", Status: service.StatusActive,
							GroupID: &group.ID, Group: group, User: user,
						}, nil
					}}
					subRepo := &stubUserSubscriptionRepo{getActive: func(_ context.Context, userID, groupID int64) (*service.UserSubscription, error) {
						if !active {
							return nil, service.ErrSubscriptionNotFound
						}
						return &service.UserSubscription{
							ID: 200, UserID: userID, GroupID: groupID,
							Status:    service.SubscriptionStatusActive,
							ExpiresAt: time.Now().Add(time.Hour),
						}, nil
					}}
					cfg := &config.Config{RunMode: config.RunModeSimple}
					apiKeys := service.NewAPIKeyService(keys, nil, nil, nil, nil, nil, cfg)
					subscriptions := service.NewSubscriptionService(nil, subRepo, nil, nil, cfg)
					t.Cleanup(subscriptions.Stop)
					calls := 0
					recorder := permissionAuthRequest(permissionAuthRouter(apiKeys, subscriptions, cfg, google, &calls))
					if active {
						require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
						require.Equal(t, 1, calls)
					} else {
						require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
						require.Zero(t, calls, "a subscription key without an entitlement must be rejected")
					}
				})
			}
		})
	}
}

func ptrToPermissionTime(value time.Time) *time.Time { return &value }

type permissionAuthUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r *permissionAuthUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	cp := *r.user
	cp.AllowedGroups = append([]int64(nil), r.user.AllowedGroups...)
	return &cp, nil
}

func (r *permissionAuthUserRepo) Update(_ context.Context, user *service.User, fields service.UserUpdateFields) error {
	if fields.RestrictPublicGroups {
		r.user.RestrictPublicGroups = user.RestrictPublicGroups
	}
	if fields.AllowedGroups {
		r.user.AllowedGroups = append([]int64(nil), user.AllowedGroups...)
	}
	return nil
}

type permissionAuthKeyRepo struct {
	*stubApiKeyRepo
	userID int64
	key    string
}

func (r *permissionAuthKeyRepo) ListKeysByUserID(_ context.Context, userID int64) ([]string, error) {
	if userID == r.userID {
		return []string{r.key}, nil
	}
	return nil, nil
}

type permissionAuthCache struct {
	service.APIKeyCache
	entries   map[string]*service.APIKeyAuthCacheEntry
	deletes   int
	publishes int
}

func (c *permissionAuthCache) GetAuthCache(_ context.Context, key string) (*service.APIKeyAuthCacheEntry, error) {
	return c.entries[key], nil
}

func (c *permissionAuthCache) SetAuthCache(_ context.Context, key string, entry *service.APIKeyAuthCacheEntry, _ time.Duration) error {
	c.entries[key] = entry
	return nil
}

func (c *permissionAuthCache) DeleteAuthCache(_ context.Context, key string) error {
	delete(c.entries, key)
	c.deletes++
	return nil
}

func (c *permissionAuthCache) PublishAuthCacheInvalidation(context.Context, string) error {
	c.publishes++
	return nil
}

func permissionAdminService(cfg *config.Config, users service.UserRepository, keys service.APIKeyRepository, invalidator service.APIKeyAuthCacheInvalidator) service.AdminService {
	return service.NewAdminService(
		cfg, users,
		nil, // group repository
		nil, // account repository
		nil, // proxy repository
		nil, // proxy group repository
		keys,
		nil, // redeem repository
		nil, // user group rates
		nil, // user RPM cache
		nil, // billing cache
		nil, // proxy prober
		nil, // proxy latency cache
		invalidator,
		nil, // ent client
		nil, // settings
		nil, // default subscriptions
		nil, // subscription repository
		nil, // privacy clients
		nil, // account runtime blocker
		nil, // affiliate service
		nil, // composite routes
		nil, // composite resolver
		nil, // channel invalidator
	)
}

func TestAPIKeyAuthPermissionAdminUpdateRevokesCachedKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, google := range []bool{false, true} {
		for _, exclusive := range []bool{false, true} {
			name := map[bool]string{false: "standard", true: "google"}[google] + "/" +
				map[bool]string{false: "restrict public", true: "revoke exclusive"}[exclusive]
			t.Run(name, func(t *testing.T) {
				users := &permissionAuthUserRepo{user: &service.User{
					ID: 42, Role: service.RoleUser, Status: service.StatusActive, Balance: 12,
				}}
				if exclusive {
					users.user.AllowedGroups = []int64{7}
				}
				group := &service.Group{ID: 7, Status: service.StatusActive, IsExclusive: exclusive, Hydrated: true}
				loads := 0
				keys := &permissionAuthKeyRepo{
					userID: 42, key: "local-permission-key",
					stubApiKeyRepo: &stubApiKeyRepo{getByKey: func(ctx context.Context, _ string) (*service.APIKey, error) {
						loads++
						user, err := users.GetByID(ctx, 42)
						if err != nil {
							return nil, err
						}
						return &service.APIKey{
							ID: 100, UserID: 42, Key: "local-permission-key", Status: service.StatusActive,
							GroupID: &group.ID, Group: group, User: user,
						}, nil
					}},
				}
				cache := &permissionAuthCache{entries: make(map[string]*service.APIKeyAuthCacheEntry)}
				cfg := &config.Config{
					RunMode:    config.RunModeStandard,
					APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 60},
				}
				apiKeys := service.NewAPIKeyService(keys, users, nil, nil, nil, cache, cfg)
				admin := permissionAdminService(cfg, users, keys, apiKeys)
				calls := 0
				router := permissionAuthRouter(apiKeys, nil, cfg, google, &calls)
				require.Equal(t, http.StatusOK, permissionAuthRequest(router).Code)
				require.Equal(t, http.StatusOK, permissionAuthRequest(router).Code)
				require.Equal(t, 1, loads, "the second request must really use the old authorization cache")
				require.Len(t, cache.entries, 1)

				input := &service.UpdateUserInput{}
				if exclusive {
					empty := []int64{}
					input.AllowedGroups = &empty
				} else {
					enabled := true
					input.RestrictPublicGroups = &enabled
				}
				_, err := admin.UpdateUser(context.Background(), 42, input)
				require.NoError(t, err)
				require.Empty(t, cache.entries)
				require.Equal(t, 1, cache.deletes)
				require.Equal(t, 1, cache.publishes, "revocation must be propagated to other instances")
				callsBefore := calls
				recorder := permissionAuthRequest(router)
				require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
				require.Equal(t, callsBefore, calls, "revoked cached keys must not reach downstream work")
				require.Equal(t, 2, loads)

				if !exclusive {
					disabled := false
					_, err = admin.UpdateUser(context.Background(), 42, &service.UpdateUserInput{
						RestrictPublicGroups: &disabled,
					})
					require.NoError(t, err)
					require.Equal(t, http.StatusOK, permissionAuthRequest(router).Code,
						"explicitly disabling the restriction restores legitimate public-group use")
				}
			})
		}
	}
}
