package service

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	chatAnywhereContextRateLimitKey = "chatanywhere:context_512k"
	chatAnywhereContextLimitTokens  = 512 * 1024
	chatAnywhereContextCooldown     = 24 * time.Hour
)

func chatAnywhereContextLimitExceeded(ctx context.Context) bool {
	tokens, ok := OpenAIRequestInputTokensFromContext(ctx)
	return ok && tokens > chatAnywhereContextLimitTokens
}

func isChatAnywhereContextQuotaExceededError(statusCode int, upstreamMsg string, responseBody []byte) bool {
	if statusCode != http.StatusForbidden {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(upstreamMsg + "\n" + extractUpstreamErrorMessage(responseBody) + "\n" + string(responseBody)))
	if text == "" {
		return false
	}
	return (strings.Contains(text, "免费点数") && strings.Contains(text, "token")) ||
		(strings.Contains(text, "额度不足") && strings.Contains(text, "token")) ||
		(strings.Contains(text, "input token") && (strings.Contains(text, "4096") || strings.Contains(text, "quota") || strings.Contains(text, "free")))
}

func isChatAnywhereContextRateLimitError(err *UpstreamFailoverError) bool {
	return err != nil && isChatAnywhereContextQuotaExceededError(err.StatusCode, "", err.ResponseBody)
}

func IsChatAnywhereContextRateLimitError(err *UpstreamFailoverError) bool {
	return isChatAnywhereContextRateLimitError(err)
}

func (s *RateLimitService) HandleChatAnywhereContextQuotaError(
	ctx context.Context,
	account *Account,
	statusCode int,
	upstreamMsg string,
	responseBody []byte,
) bool {
	if s == nil || s.accountRepo == nil || account == nil || account.Platform != PlatformChatAnywhere ||
		!isChatAnywhereContextQuotaExceededError(statusCode, upstreamMsg, responseBody) {
		return false
	}

	resetAt := time.Now().UTC().Add(chatAnywhereContextCooldown)
	if err := s.accountRepo.SetModelRateLimit(
		ctx,
		account.ID,
		chatAnywhereContextRateLimitKey,
		resetAt,
		"chatanywhere_context_quota_403",
	); err != nil {
		slog.Warn("chatanywhere_context_quota_cooldown_failed", "account_id", account.ID, "error", err)
		return true
	}

	// This cooldown is request-scoped. Clear stale in-memory account blocks so
	// requests within the allowed context size can continue to use the key.
	s.notifyAccountSchedulingBlockCleared(account.ID)
	slog.Warn(
		"chatanywhere_context_quota_cooldown_set",
		"account_id", account.ID,
		"reset_at", resetAt,
		"context_limit_tokens", chatAnywhereContextLimitTokens,
	)
	return true
}
