package service

import (
	"context"
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
		PlatformGrok, PlatformAgnes, PlatformDeepSeek, PlatformNvidia, PlatformTokenRhythm,
		PlatformKimi, PlatformZhipu, PlatformChatAnywhere, PlatformGLM, PlatformModelScope,
		PlatformDashScope, PlatformMiniMax, PlatformVolcengine:
		return nil
	default:
		return infraerrors.BadRequest("UNSUPPORTED_ACCOUNT_PLATFORM", "unsupported account platform")
	}
}

func ValidateGroupPlatform(platform string) error {
	switch strings.TrimSpace(platform) {
	case PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity,
		PlatformGrok, PlatformAgnes, PlatformDeepSeek, PlatformNvidia, PlatformTokenRhythm,
		PlatformKimi, PlatformZhipu, PlatformChatAnywhere, PlatformGLM, PlatformModelScope,
		PlatformDashScope, PlatformMiniMax, PlatformVolcengine, PlatformComposite:
		return nil
	default:
		return infraerrors.BadRequest("UNSUPPORTED_GROUP_PLATFORM", "unsupported group platform")
	}
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
		if group == nil {
			return ErrGroupNotFound
		}
	}
	return nil
}

func validateAccountCredentials(platform, accountType string, credentials map[string]any) error {
	platform = strings.TrimSpace(platform)
	if err := ValidateAccountPlatform(platform); err != nil {
		return err
	}
	if platform != PlatformDeepSeek && platform != PlatformNvidia && platform != PlatformTokenRhythm && platform != PlatformKimi && platform != PlatformZhipu && platform != PlatformChatAnywhere && platform != PlatformGLM && platform != PlatformModelScope && platform != PlatformDashScope && platform != PlatformMiniMax && platform != PlatformVolcengine {
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
	if platform == PlatformKimi {
		platformName = "Kimi"
		errorPrefix = "KIMI"
	}
	if platform == PlatformZhipu {
		platformName = "Zhipu"
		errorPrefix = "ZHIPU"
	}
	if platform == PlatformChatAnywhere {
		platformName = "ChatAnywhere"
		errorPrefix = "CHATANYWHERE"
	}
	if platform == PlatformGLM {
		platformName = "GLM"
		errorPrefix = "GLM"
	}
	if platform == PlatformModelScope {
		platformName = "ModelScope"
		errorPrefix = "MODELSCOPE"
	}
	if platform == PlatformDashScope {
		platformName = "DashScope"
		errorPrefix = "DASHSCOPE"
	}
	if platform == PlatformMiniMax {
		platformName = "MiniMax"
		errorPrefix = "MINIMAX"
	}
	if platform == PlatformVolcengine {
		platformName = "Volcengine"
		errorPrefix = "VOLCENGINE"
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
	if platform == PlatformModelScope || platform == PlatformDashScope || platform == PlatformMiniMax || platform == PlatformVolcengine {
		if rawBaseURL, exists := credentials["base_url"]; exists && rawBaseURL != nil {
			baseURL, ok := rawBaseURL.(string)
			if !ok {
				return infraerrors.BadRequest(errorPrefix+"_BASE_URL_INVALID", platformName+" base_url must be an absolute http(s) URL")
			}
			if strings.TrimSpace(baseURL) != "" {
				parsed, err := url.ParseRequestURI(strings.TrimSpace(baseURL))
				if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
					return infraerrors.BadRequest(errorPrefix+"_BASE_URL_INVALID", platformName+" base_url must be an absolute http(s) URL")
				}
			}
		}
		return nil
	}
	if platform == PlatformGLM {
		if rawBaseURL, exists := credentials["base_url"]; exists && rawBaseURL != nil {
			baseURL, ok := rawBaseURL.(string)
			if !ok {
				return infraerrors.BadRequest(errorPrefix+"_BASE_URL_INVALID", platformName+" base_url must be a supported GLM endpoint")
			}
			trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
			if trimmed != "" && trimmed != GLMDefaultBaseURL {
				return infraerrors.BadRequest("GLM_BASE_URL_INVALID", "GLM base_url must use the mainland Zhipu AI Open Platform endpoint")
			}
		}
		return nil
	}
	if platform == PlatformChatAnywhere {
		if rawBaseURL, exists := credentials["base_url"]; exists && rawBaseURL != nil {
			baseURL, ok := rawBaseURL.(string)
			if !ok {
				return infraerrors.BadRequest(errorPrefix+"_BASE_URL_INVALID", platformName+" base_url must be a supported ChatAnywhere endpoint")
			}
			trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
			if trimmed != "" && trimmed != ChatAnywhereChinaBaseURL && trimmed != ChatAnywhereGlobalBaseURL {
				return infraerrors.BadRequest("CHATANYWHERE_BASE_URL_INVALID", "ChatAnywhere base_url must use a supported regional endpoint")
			}
		}
		return nil
	}
	if platform == PlatformKimi {
		if rawBaseURL, exists := credentials["base_url"]; exists && rawBaseURL != nil {
			baseURL, ok := rawBaseURL.(string)
			if !ok {
				return infraerrors.BadRequest(errorPrefix+"_BASE_URL_INVALID", platformName+" base_url must be an absolute URL")
			}
			if strings.TrimSpace(baseURL) != "" {
				parsed, err := url.ParseRequestURI(strings.TrimSpace(baseURL))
				if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
					return infraerrors.BadRequest(errorPrefix+"_BASE_URL_INVALID", platformName+" base_url must be an absolute http(s) URL")
				}
			}
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

func isKimiBaseURL(raw string) bool {
	baseURL := strings.TrimRight(strings.TrimSpace(raw), "/")
	return baseURL == KimiDefaultBaseURL || baseURL == KimiInternationalBaseURL
}
