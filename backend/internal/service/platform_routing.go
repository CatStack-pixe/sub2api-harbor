package service

import "strings"

// accountPlatformsForGroupPlatform returns the concrete account platforms
// eligible for a grouped request. OpenAI-compatible groups share the complete
// account pool; native protocol groups retain their protocol-specific pool.
func accountPlatformsForGroupPlatform(groupPlatform string) []string {
	groupPlatform = strings.TrimSpace(groupPlatform)
	if ValidateGroupPlatform(groupPlatform) != nil {
		return nil
	}
	switch groupPlatform {
	case PlatformAnthropic, PlatformGemini, PlatformAntigravity:
		return []string{groupPlatform}
	case PlatformComposite:
		platforms := schedulerSnapshotPlatforms()
		return append([]string(nil), platforms[:]...)
	}
	return openAICompatibleGroupPlatforms()
}

func openAICompatibleGroupPlatforms() []string {
	allPlatforms := schedulerSnapshotPlatforms()
	platforms := make([]string, 0, len(allPlatforms))
	for _, platform := range allPlatforms {
		if (&Account{Platform: platform}).IsOpenAICompatible() {
			platforms = append(platforms, platform)
		}
	}
	return platforms
}

func accountPlatformMatchesGroup(groupPlatform, accountPlatform string) bool {
	groupPlatform = strings.TrimSpace(groupPlatform)
	accountPlatform = strings.TrimSpace(accountPlatform)
	if ValidateGroupPlatform(groupPlatform) != nil {
		return false
	}
	if !isConcreteRequestPlatform(accountPlatform) {
		return false
	}

	// Native Anthropic/Gemini transports (including direct Antigravity groups)
	// must keep their protocol-specific account pool. Mixed scheduling adds an
	// explicitly enabled Antigravity account at the call site.
	switch groupPlatform {
	case PlatformAnthropic, PlatformGemini, PlatformAntigravity:
		return accountPlatform == groupPlatform
	case PlatformComposite:
		return true
	default:
		// All remaining groups use the OpenAI-compatible gateway. The account's
		// concrete platform selects the upstream adapter and credentials.
		return (&Account{Platform: accountPlatform}).IsOpenAICompatible()
	}
}
