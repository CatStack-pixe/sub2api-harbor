package admin

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/gin-gonic/gin"
)

func setAdminSensitiveCacheHeaders(c *gin.Context, vary ...string) {
	c.Header("Cache-Control", "private, no-store")
	if len(vary) == 0 {
		vary = []string{"Authorization", "X-API-Key"}
	}
	fields := c.Writer.Header().Values("Vary")
	for _, requested := range vary {
		found := false
		for _, existing := range fields {
			for _, field := range strings.Split(existing, ",") {
				if strings.EqualFold(strings.TrimSpace(field), requested) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			fields = append(fields, requested)
		}
	}
	c.Writer.Header().Set("Vary", strings.Join(fields, ", "))
}

type scopedUserUsageTrendPoint struct {
	Date       string  `json:"date"`
	UserID     int64   `json:"user_id"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
	Cost       float64 `json:"cost"`
	ActualCost float64 `json:"actual_cost"`
}

func adminUserUsageTrendResponse(c *gin.Context, points []usagestats.UserUsageTrendPoint) any {
	if !scopedAdminWriteOnly(c, "users.read") {
		return points
	}
	if points == nil {
		return points
	}
	out := make([]scopedUserUsageTrendPoint, 0, len(points))
	for _, point := range points {
		out = append(out, scopedUserUsageTrendPoint{
			Date:       point.Date,
			UserID:     point.UserID,
			Requests:   point.Requests,
			Tokens:     point.Tokens,
			Cost:       point.Cost,
			ActualCost: point.ActualCost,
		})
	}
	return out
}

type scopedDashboardSnapshotV2Response struct {
	GeneratedAt string `json:"generated_at"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Granularity string `json:"granularity"`

	Stats      *dashboardSnapshotV2Stats   `json:"stats,omitempty"`
	Trend      []usagestats.TrendDataPoint `json:"trend,omitempty"`
	Models     []usagestats.ModelStat      `json:"models,omitempty"`
	Groups     []usagestats.GroupStat      `json:"groups,omitempty"`
	UsersTrend any                         `json:"users_trend,omitempty"`
}

func adminDashboardSnapshotV2Response(c *gin.Context, source *dashboardSnapshotV2Response) any {
	if source == nil {
		return (*scopedDashboardSnapshotV2Response)(nil)
	}
	if !scopedAdminWriteOnly(c, "users.read") {
		return source
	}
	var usersTrend any
	if source.UsersTrend != nil {
		usersTrend = adminUserUsageTrendResponse(c, source.UsersTrend)
	}
	return &scopedDashboardSnapshotV2Response{
		GeneratedAt: source.GeneratedAt,
		StartDate:   source.StartDate,
		EndDate:     source.EndDate,
		Granularity: source.Granularity,
		Stats:       source.Stats,
		Trend:       source.Trend,
		Models:      source.Models,
		Groups:      source.Groups,
		UsersTrend:  usersTrend,
	}
}

type dashboardUserSpendingRankingResponse struct {
	Ranking         []usagestats.UserSpendingRankingItem `json:"ranking"`
	TotalActualCost float64                              `json:"total_actual_cost"`
	TotalRequests   int64                                `json:"total_requests"`
	TotalTokens     int64                                `json:"total_tokens"`
	StartDate       string                               `json:"start_date"`
	EndDate         string                               `json:"end_date"`
}

type scopedUserSpendingRankingItem struct {
	UserID     int64   `json:"user_id"`
	ActualCost float64 `json:"actual_cost"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
}

func adminUserSpendingRankingResponse(c *gin.Context, payload dashboardUserSpendingRankingResponse) any {
	if !scopedAdminWriteOnly(c, "users.read") {
		return payload
	}
	var ranking []scopedUserSpendingRankingItem
	if payload.Ranking != nil {
		ranking = make([]scopedUserSpendingRankingItem, 0, len(payload.Ranking))
		for _, item := range payload.Ranking {
			ranking = append(ranking, scopedUserSpendingRankingItem{
				UserID:     item.UserID,
				ActualCost: item.ActualCost,
				Requests:   item.Requests,
				Tokens:     item.Tokens,
			})
		}
	}
	payloadView := struct {
		Ranking         []scopedUserSpendingRankingItem `json:"ranking"`
		TotalActualCost float64                         `json:"total_actual_cost"`
		TotalRequests   int64                           `json:"total_requests"`
		TotalTokens     int64                           `json:"total_tokens"`
		StartDate       string                          `json:"start_date"`
		EndDate         string                          `json:"end_date"`
	}{
		Ranking:         ranking,
		TotalActualCost: payload.TotalActualCost,
		TotalRequests:   payload.TotalRequests,
		TotalTokens:     payload.TotalTokens,
		StartDate:       payload.StartDate,
		EndDate:         payload.EndDate,
	}
	return payloadView
}

type scopedUserBreakdownItem struct {
	UserID       int64   `json:"user_id"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CacheTokens  int64   `json:"cache_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	Cost         float64 `json:"cost"`
	ActualCost   float64 `json:"actual_cost"`
	AccountCost  float64 `json:"account_cost"`
}

func adminUserBreakdownResponse(c *gin.Context, users []usagestats.UserBreakdownItem) any {
	if !scopedAdminWriteOnly(c, "users.read") {
		return users
	}
	if users == nil {
		return users
	}
	out := make([]scopedUserBreakdownItem, 0, len(users))
	for _, user := range users {
		out = append(out, scopedUserBreakdownItem{
			UserID:       user.UserID,
			Requests:     user.Requests,
			InputTokens:  user.InputTokens,
			OutputTokens: user.OutputTokens,
			CacheTokens:  user.CacheTokens,
			TotalTokens:  user.TotalTokens,
			Cost:         user.Cost,
			ActualCost:   user.ActualCost,
			AccountCost:  user.AccountCost,
		})
	}
	return out
}
