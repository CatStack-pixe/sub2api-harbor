package service

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const chatAnywhereFreePointsQuotaReasonPrefix = "chatanywhere_free_points_quota"

func isChatAnywhereFreePointsQuotaExceededError(statusCode int, upstreamMsg string, responseBody []byte) bool {
	if statusCode != http.StatusForbidden {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(upstreamMsg + "\n" + extractUpstreamErrorMessage(responseBody) + "\n" + string(responseBody)))
	if text == "" {
		return false
	}

	chinese := strings.Contains(text, "7天") &&
		strings.Contains(text, "免费点数") &&
		(strings.Contains(text, "不足") || strings.Contains(text, "不够") || strings.Contains(text, "耗尽"))
	english := strings.Contains(text, "free points") &&
		(strings.Contains(text, "7-day") || strings.Contains(text, "7 day") || strings.Contains(text, "weekly")) &&
		(strings.Contains(text, "insufficient") || strings.Contains(text, "not enough") || strings.Contains(text, "exhausted"))
	return chinese || english
}

// HandleChatAnywhereFreePointsQuotaError quarantines a key for the provider's
// rolling seven-day free-points window without converting a recoverable quota
// response into a permanent account error.
func (s *RateLimitService) HandleChatAnywhereFreePointsQuotaError(
	ctx context.Context,
	account *Account,
	statusCode int,
	upstreamMsg string,
	responseBody []byte,
) bool {
	if s == nil || s.accountRepo == nil || account == nil || !account.IsChatAnywhere() ||
		!isChatAnywhereFreePointsQuotaExceededError(statusCode, upstreamMsg, responseBody) {
		return false
	}

	message := strings.TrimSpace(upstreamMsg)
	if message == "" {
		message = strings.TrimSpace(extractUpstreamErrorMessage(responseBody))
	}
	message = sanitizeUpstreamErrorMessage(message)
	if message == "" {
		message = "free points in the current 7-day window are insufficient"
	}
	reason := chatAnywhereFreePointsQuotaReasonPrefix + ": " + truncateForLog([]byte(message), 512)
	until := time.Now().Add(chatAnywhereFreePointsWindow)

	s.notifyAccountSchedulingBlocked(account, until, chatAnywhereFreePointsQuotaReasonPrefix)
	if err := s.accountRepo.SetTempUnschedulable(ctx, account.ID, until, reason); err != nil {
		slog.Warn("chatanywhere_free_points_set_temp_unschedulable_failed", "account_id", account.ID, "error", err)
		return true
	}

	slog.Warn(
		"chatanywhere_free_points_temp_unschedulable",
		"account_id", account.ID,
		"until", until.UTC(),
	)
	return true
}
