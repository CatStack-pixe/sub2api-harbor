package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// ValidateHeartbeatAccount performs the post-provision readiness check. Public
// balance endpoints are used where they exist; the remaining API-key
// platforms use their authenticated model-list endpoint as a lightweight
// availability probe.
func (s *AccountTestService) ValidateHeartbeatAccount(ctx context.Context, accountID int64, expectedPlatform string) error {
	if s == nil || s.accountRepo == nil {
		return fmt.Errorf("heartbeat account validation is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account == nil {
		return fmt.Errorf("heartbeat account %d was not found", accountID)
	}
	if expectedPlatform != "" && account.Platform != expectedPlatform {
		return fmt.Errorf("heartbeat account platform %q does not match %q", account.Platform, expectedPlatform)
	}

	switch account.Platform {
	case PlatformDeepSeek:
		result, err := s.FetchDeepSeekBalance(ctx, accountID)
		if err != nil {
			return err
		}
		if result == nil || !result.IsAvailable {
			return fmt.Errorf("DeepSeek balance is unavailable")
		}
	case PlatformKimi:
		if account.IsCodingPlan() {
			return s.validateHeartbeatModels(ctx, account)
		}
		result, err := s.FetchKimiBalance(ctx, accountID)
		if err != nil {
			return err
		}
		if result == nil || !result.IsAvailable {
			return fmt.Errorf("kimi balance is unavailable")
		}
	default:
		return s.validateHeartbeatModels(ctx, account)
	}
	return nil
}

func (s *AccountTestService) validateHeartbeatModels(ctx context.Context, account *Account) error {
	if account == nil || account.Type != AccountTypeAPIKey {
		return fmt.Errorf("heartbeat provider requires an API key account")
	}
	if s.httpUpstream == nil {
		return fmt.Errorf("upstream HTTP client is not configured")
	}
	req, err := s.buildUpstreamModelsRequest(ctx, account)
	if err != nil {
		return fmt.Errorf("build heartbeat availability probe: %w", err)
	}
	resp, err := s.doUpstreamModelsRequest(req, upstreamModelsProxyURL(account), account)
	if err != nil {
		return fmt.Errorf("heartbeat availability probe failed: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("heartbeat availability probe returned no response")
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.CopyN(io.Discard, resp.Body, 256*1024)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		// Some API-key providers do not publish a model-list endpoint. Account
		// creation remains valid; subsequent gateway requests perform auth.
		return nil
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("heartbeat availability probe returned HTTP %d", resp.StatusCode)
	}
	return nil
}
