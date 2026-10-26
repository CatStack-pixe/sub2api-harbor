package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Metrics permissions do not grant access to the associated users' profiles.
// Explicit projections keep future service fields behind users.read as well.
type scopedGroupRateResponse struct {
	UserID         int64    `json:"user_id"`
	RateMultiplier *float64 `json:"rate_multiplier,omitempty"`
	RPMOverride    *int     `json:"rpm_override,omitempty"`
}

func adminGroupRateMultipliersResponse(c *gin.Context, entries []service.UserGroupRateEntry) any {
	if !scopedAdminWriteOnly(c, "users.read") {
		return entries
	}
	out := make([]scopedGroupRateResponse, 0, len(entries))
	for _, entry := range entries {
		out = append(out, scopedGroupRateResponse{
			UserID:         entry.UserID,
			RateMultiplier: entry.RateMultiplier,
			RPMOverride:    entry.RPMOverride,
		})
	}
	return out
}

type scopedUserConcurrencyResponse struct {
	UserID         int64   `json:"user_id"`
	CurrentInUse   int64   `json:"current_in_use"`
	MaxCapacity    int64   `json:"max_capacity"`
	LoadPercentage float64 `json:"load_percentage"`
	WaitingInQueue int64   `json:"waiting_in_queue"`
}

func adminUserConcurrencyResponse(c *gin.Context, users map[int64]*service.UserConcurrencyInfo) any {
	if !scopedAdminWriteOnly(c, "users.read") {
		return users
	}
	out := make(map[int64]*scopedUserConcurrencyResponse, len(users))
	for id, user := range users {
		if user == nil {
			out[id] = nil
			continue
		}
		out[id] = &scopedUserConcurrencyResponse{
			UserID:         user.UserID,
			CurrentInUse:   user.CurrentInUse,
			MaxCapacity:    user.MaxCapacity,
			LoadPercentage: user.LoadPercentage,
			WaitingInQueue: user.WaitingInQueue,
		}
	}
	return out
}
