//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type scopedMetricsGroups struct {
	service.AdminService
	entries []service.UserGroupRateEntry
}

func (s *scopedMetricsGroups) GetGroupRateMultipliers(context.Context, int64) ([]service.UserGroupRateEntry, error) {
	return s.entries, nil
}

type scopedMetricsUsers struct {
	service.UserRepository
	users []service.User
}

func (r *scopedMetricsUsers) ListWithFilters(context.Context, pagination.PaginationParams, service.UserListFilters) ([]service.User, *pagination.PaginationResult, error) {
	return r.users, &pagination.PaginationResult{Total: int64(len(r.users))}, nil
}

type scopedMetricsConcurrency struct {
	service.ConcurrencyCache
}

func (scopedMetricsConcurrency) GetUsersLoadBatch(_ context.Context, users []service.UserWithConcurrency) (map[int64]*service.UserLoadInfo, error) {
	out := make(map[int64]*service.UserLoadInfo, len(users))
	for _, user := range users {
		out[user.ID] = &service.UserLoadInfo{UserID: user.ID, CurrentConcurrency: 2, WaitingCount: 1}
	}
	return out, nil
}

// Exercise the real handlers and OpsService; only repositories and the load
// cache are fixtures, so missing response projections fail at the HTTP boundary.
func TestScopedAdminMetricsHandlersUserResponseBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, identity := range []struct {
		name     string
		scoped   bool
		userRead bool
	}{
		{name: "scoped_metrics_only", scoped: true},
		{name: "scoped_with_users_read", scoped: true, userRead: true},
		{name: "admin_jwt"},
	} {
		for _, empty := range []bool{false, true} {
			name := identity.name + "/populated"
			if empty {
				name = identity.name + "/empty"
			}
			t.Run(name, func(t *testing.T) {
				rate, rpm := 1.25, 0
				groups := &scopedMetricsGroups{}
				users := &scopedMetricsUsers{}
				if !empty {
					groups.entries = []service.UserGroupRateEntry{
						{UserID: 7, UserName: "private-username", UserEmail: "private@example.invalid", UserNotes: "private-notes", UserStatus: "active", RateMultiplier: &rate, RPMOverride: &rpm},
						{UserID: 8, UserName: "private-second-user", UserEmail: "second@example.invalid", UserNotes: "private-second-notes", UserStatus: "disabled"},
					}
					users.users = []service.User{{ID: 7, Email: "private@example.invalid", Username: "private-username", Notes: "private-notes", Status: service.StatusActive, Concurrency: 4}}
				}
				groupHandler := NewGroupHandler(groups, nil, nil)
				concurrency := service.NewConcurrencyService(scopedMetricsConcurrency{})
				opsHandler := NewOpsHandler(service.NewOpsService(nil, nil, nil, nil, users, concurrency, nil, nil, nil, nil, nil))
				r := gin.New()
				r.Use(func(c *gin.Context) {
					c.Set("auth_method", "jwt")
					if identity.scoped {
						permissions := []string{"groups.read", "ops.read"}
						if identity.userRead {
							permissions = append(permissions, "users.read")
						}
						c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), &adminaccess.Principal{Policy: adminaccess.Policy{GlobalPermissions: permissions}}))
						c.Set("auth_method", "admin_api_key")
					}
					c.Next()
				})
				r.GET("/api/v1/admin/groups/:id/rate-multipliers", groupHandler.ScopedAdminKeyGroupModels, groupHandler.GetGroupRateMultipliers)
				r.GET("/api/v1/admin/ops/user-concurrency", opsHandler.GetUserConcurrencyStats)
				restricted := identity.scoped && !identity.userRead
				for _, path := range []string{"/api/v1/admin/groups/1/rate-multipliers", "/api/v1/admin/ops/user-concurrency"} {
					w := httptest.NewRecorder()
					r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
					require.Equal(t, 200, w.Code, path+": "+w.Body.String())
					var body struct {
						Data json.RawMessage `json:"data"`
					}
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
					if path == "/api/v1/admin/groups/1/rate-multipliers" {
						switch {
						case empty:
							require.JSONEq(t, `[]`, string(body.Data))
						case restricted:
							require.JSONEq(t, `[{"user_id":7,"rate_multiplier":1.25,"rpm_override":0},{"user_id":8}]`, string(body.Data))
						default:
							expected, err := json.Marshal(groups.entries)
							require.NoError(t, err)
							require.JSONEq(t, string(expected), string(body.Data))
						}
					} else {
						var payload struct {
							Enabled   bool            `json:"enabled"`
							User      json.RawMessage `json:"user"`
							Timestamp string          `json:"timestamp"`
						}
						require.NoError(t, json.Unmarshal(body.Data, &payload))
						require.True(t, payload.Enabled)
						require.NotEmpty(t, payload.Timestamp)
						switch {
						case empty:
							require.JSONEq(t, `{}`, string(payload.User))
						case restricted:
							require.JSONEq(t, `{"7":{"user_id":7,"current_in_use":2,"max_capacity":4,"load_percentage":50,"waiting_in_queue":1}}`, string(payload.User))
						default:
							require.JSONEq(t, `{"7":{"user_id":7,"user_email":"private@example.invalid","username":"private-username","current_in_use":2,"max_capacity":4,"load_percentage":50,"waiting_in_queue":1}}`, string(payload.User))
						}
					}
					if restricted {
						require.NotContains(t, w.Body.String(), "private")
						for _, field := range []string{"user_email", "email", "user_name", "username", "user_notes", "notes", "user_status", "status"} {
							require.NotContains(t, string(body.Data), `"`+field+`"`)
						}
					}
				}
				if !empty {
					require.Equal(t, "private@example.invalid", groups.entries[0].UserEmail, "projection must not mutate source records")
					require.Equal(t, "private-notes", groups.entries[0].UserNotes)
					require.Equal(t, "private-username", users.users[0].Username)
				}
			})
		}
	}
}
