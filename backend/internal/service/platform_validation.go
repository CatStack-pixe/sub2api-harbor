package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ValidateAccountPlatform keeps account platform values aligned with the
// routing platforms understood by the gateway. Composite is a group-only
// platform and must never be stored on an account.
func ValidateAccountPlatform(platform string) error {
	switch strings.TrimSpace(platform) {
	case PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity,
		PlatformGrok, PlatformAgnes, PlatformDeepSeek, PlatformNvidia, PlatformTokenRhythm:
		return nil
	default:
		return infraerrors.BadRequest("UNSUPPORTED_ACCOUNT_PLATFORM", "unsupported account platform")
	}
}

func ValidateGroupPlatform(platform string) error {
	switch strings.TrimSpace(platform) {
	case PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity,
		PlatformGrok, PlatformAgnes, PlatformDeepSeek, PlatformNvidia, PlatformTokenRhythm, PlatformComposite:
		return nil
	default:
		return infraerrors.BadRequest("UNSUPPORTED_GROUP_PLATFORM", "unsupported group platform")
	}
}

func accountCanBindToGroupPlatform(accountPlatform, groupPlatform string) bool {
	accountPlatform = strings.TrimSpace(accountPlatform)
	groupPlatform = strings.TrimSpace(groupPlatform)
	if accountPlatform != PlatformDeepSeek && accountPlatform != PlatformNvidia && accountPlatform != PlatformTokenRhythm &&
		groupPlatform != PlatformDeepSeek && groupPlatform != PlatformNvidia && groupPlatform != PlatformTokenRhythm {
		return true
	}
	return groupPlatform == PlatformComposite || accountPlatform == groupPlatform
}

func validateAccountGroupPlatforms(ctx context.Context, groups GroupRepository, accountPlatform string, groupIDs []int64) error {
	if groups == nil || len(groupIDs) == 0 {
		return nil
	}
	for _, groupID := range groupIDs {
		group, err := groups.GetByID(ctx, groupID)
		if err != nil {
			return err
		}
		if group != nil && !accountCanBindToGroupPlatform(accountPlatform, group.Platform) {
			return infraerrors.BadRequest(
				"ACCOUNT_GROUP_PLATFORM_MISMATCH",
				fmt.Sprintf("account platform %q cannot be bound to group platform %q", accountPlatform, group.Platform),
			)
		}
	}
	return nil
}

func validateGroupAccountPlatforms(ctx context.Context, accounts AccountRepository, groupPlatform string, groupID int64) error {
	if accounts == nil || groupID <= 0 {
		return nil
	}
	boundAccounts, err := accounts.ListByGroup(ctx, groupID)
	if err != nil {
		return err
	}
	for _, account := range boundAccounts {
		if !accountCanBindToGroupPlatform(account.Platform, groupPlatform) {
			return infraerrors.BadRequest(
				"ACCOUNT_GROUP_PLATFORM_MISMATCH",
				fmt.Sprintf("account platform %q cannot remain bound to group platform %q", account.Platform, groupPlatform),
			)
		}
	}
	return nil
}

func validateAccountCredentials(platform, accountType string, credentials map[string]any) error {
	platform = strings.TrimSpace(platform)
	if err := ValidateAccountPlatform(platform); err != nil {
		return err
	}
	if platform != PlatformDeepSeek && platform != PlatformNvidia && platform != PlatformTokenRhythm {
		return nil
	}
	platformName := "DeepSeek"
	errorPrefix := "DEEPSEEK"
	if platform == PlatformNvidia {
		platformName = "NVIDIA"
		errorPrefix = "NVIDIA"
	}
	if platform == PlatformTokenRhythm {
		platformName = "TokenRhythm"
		errorPrefix = "TOKENRHYTHM"
	}
	if accountType != AccountTypeAPIKey {
		return infraerrors.BadRequest(errorPrefix+"_ACCOUNT_TYPE_UNSUPPORTED", platformName+" accounts must use the apikey account type")
	}
	apiKey, _ := credentials["api_key"].(string)
	if strings.TrimSpace(apiKey) == "" {
		return infraerrors.BadRequest(errorPrefix+"_API_KEY_REQUIRED", platformName+" api_key is required")
	}
	if platform == PlatformTokenRhythm {
		if rawBaseURL, exists := credentials["base_url"]; exists && rawBaseURL != nil {
			baseURL, ok := rawBaseURL.(string)
			if !ok {
				return infraerrors.BadRequest(errorPrefix+"_BASE_URL_INVALID", platformName+" base_url is fixed")
			}
			trimmedBaseURL := strings.TrimSpace(baseURL)
			if trimmedBaseURL != "" && strings.TrimRight(trimmedBaseURL, "/") != TokenRhythmDefaultBaseURL {
				return infraerrors.BadRequest(errorPrefix+"_BASE_URL_INVALID", platformName+" base_url is fixed")
			}
		}
		if _, _, err := TokenRhythmCookieCredentials(credentials); err != nil {
			return infraerrors.BadRequest(errorPrefix+"_COOKIE_INVALID", err.Error())
		}
		return nil
	}
	if rawBaseURL, exists := credentials["base_url"]; exists && rawBaseURL != nil {
		baseURL, ok := rawBaseURL.(string)
		if !ok {
			return infraerrors.BadRequest(errorPrefix+"_BASE_URL_INVALID", platformName+" base_url must be an absolute URL")
		}
		if strings.TrimSpace(baseURL) == "" {
			return nil
		}
		parsed, err := url.ParseRequestURI(strings.TrimSpace(baseURL))
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return infraerrors.BadRequest(errorPrefix+"_BASE_URL_INVALID", platformName+" base_url must be an absolute URL")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return infraerrors.BadRequest(errorPrefix+"_BASE_URL_INVALID", platformName+" base_url must use http or https")
		}
	}
	return nil
}
