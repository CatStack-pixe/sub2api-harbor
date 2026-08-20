package service

// accountPlatformsForGroupPlatform keeps the group's public routing identity
// separate from compatible provider account identities.
func accountPlatformsForGroupPlatform(groupPlatform string) []string {
	if groupPlatform == PlatformOpenAI {
		return []string{PlatformOpenAI, PlatformChatAnywhere}
	}
	if groupPlatform == PlatformDeepSeek {
		return []string{PlatformDeepSeek, PlatformTokenRhythm}
	}
	return []string{groupPlatform}
}

func accountPlatformMatchesGroup(groupPlatform, accountPlatform string) bool {
	for _, platform := range accountPlatformsForGroupPlatform(groupPlatform) {
		if accountPlatform == platform {
			return true
		}
	}
	return false
}
