package service

import (
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const agnesInsufficientQuotaCode = "insufficient_user_quota"

var agnesQuotaResetLocation = time.FixedZone("Asia/Singapore", 8*60*60)

type agnesQuotaFallbackKey struct {
	accountID int64
	model     string
}

func isAgnesInsufficientQuotaResponse(account *Account, statusCode int, responseBody []byte) bool {
	if account == nil || !account.IsAgnes() || (statusCode != http.StatusPaymentRequired && statusCode != http.StatusForbidden) {
		return false
	}
	code := strings.ToLower(strings.TrimSpace(gjson.GetBytes(responseBody, "error.code").String()))
	return code == agnesInsufficientQuotaCode
}

func nextAgnesQuotaReset(now time.Time) time.Time {
	localNow := now.In(agnesQuotaResetLocation)
	return time.Date(localNow.Year(), localNow.Month(), localNow.Day()+1, 0, 0, 0, 0, agnesQuotaResetLocation)
}

func normalizeAgnesQuotaModel(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func (s *OpenAIGatewayService) resolveAgnesQuotaFallbackModel(account *Account, primaryModel string, now time.Time) (string, bool) {
	primaryModel = strings.TrimSpace(primaryModel)
	if s == nil || account == nil || !account.IsAgnes() || primaryModel == "" || strings.EqualFold(primaryModel, AgnesDefaultModel) {
		return primaryModel, false
	}

	key := agnesQuotaFallbackKey{accountID: account.ID, model: normalizeAgnesQuotaModel(primaryModel)}
	rawUntil, ok := s.agnesQuotaFallbackUntil.Load(key)
	if !ok {
		return primaryModel, false
	}
	until, ok := rawUntil.(time.Time)
	if !ok || !now.Before(until) {
		s.agnesQuotaFallbackUntil.Delete(key)
		return primaryModel, false
	}
	return AgnesDefaultModel, true
}

func (s *OpenAIGatewayService) activateAgnesQuotaFallback(account *Account, primaryModel string, statusCode int, responseBody []byte, now time.Time) bool {
	primaryModel = strings.TrimSpace(primaryModel)
	if s == nil || strings.EqualFold(primaryModel, AgnesDefaultModel) || !isAgnesInsufficientQuotaResponse(account, statusCode, responseBody) {
		return false
	}

	until := nextAgnesQuotaReset(now)
	key := agnesQuotaFallbackKey{accountID: account.ID, model: normalizeAgnesQuotaModel(primaryModel)}
	s.agnesQuotaFallbackUntil.Store(key, until)
	logger.L().Info("agnes quota exhausted: falling back to default model",
		zap.Int64("account_id", account.ID),
		zap.String("primary_model", primaryModel),
		zap.String("fallback_model", AgnesDefaultModel),
		zap.Time("retry_primary_at", until),
	)
	return true
}
