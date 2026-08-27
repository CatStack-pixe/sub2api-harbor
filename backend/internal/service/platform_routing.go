package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

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

// accountPlatformMatchesExplicitGroup scopes an explicit group by membership;
// OpenAI-compatible provider identity is intentionally not part of that scope.
func accountPlatformMatchesExplicitGroup(groupPlatform, accountPlatform string) bool {
	if isOpenAICompatibleRoutingPlatform(groupPlatform) {
		return isOpenAICompatibleRoutingPlatform(accountPlatform)
	}
	return accountPlatformMatchesGroup(groupPlatform, accountPlatform)
}

func openAICompatibleRoutingPlatforms() []string {
	return []string{
		PlatformOpenAI,
		PlatformGrok,
		PlatformAgnes,
		PlatformDeepSeek,
		PlatformNvidia,
		PlatformTokenRhythm,
		PlatformKimi,
		PlatformChatAnywhere,
	}
}

func isOpenAICompatibleRoutingPlatform(platform string) bool {
	switch platform {
	case PlatformOpenAI, PlatformGrok, PlatformAgnes, PlatformDeepSeek,
		PlatformNvidia, PlatformTokenRhythm, PlatformKimi, PlatformChatAnywhere:
		return true
	default:
		return false
	}
}

// accountPlatformMatchesSchedulingScope keeps explicit group scheduling scoped
// by membership while allowing any OpenAI-compatible provider in that group.
// A group-less request retains the existing platform mapping rules.
func accountPlatformMatchesSchedulingScope(groupID *int64, groupPlatform, accountPlatform string) bool {
	if groupID != nil && isOpenAICompatibleRoutingPlatform(groupPlatform) {
		return accountPlatformMatchesExplicitGroup(groupPlatform, accountPlatform)
	}
	if groupID == nil && isOpenAICompatibleRoutingPlatform(groupPlatform) {
		return groupPlatform == accountPlatform
	}
	return accountPlatformMatchesGroup(groupPlatform, accountPlatform)
}

// withAccountPlatformForChannelLookup selects the concrete compatible provider
// when a group-level channel restriction is evaluated for a specific account.
func withAccountPlatformForChannelLookup(ctx context.Context, channelService *ChannelService, groupID int64, account *Account) context.Context {
	if channelService == nil || account == nil || !isOpenAICompatibleRoutingPlatform(account.Platform) {
		return ctx
	}
	groupPlatform := channelService.GetGroupPlatform(ctx, groupID)
	if !isOpenAICompatibleRoutingPlatform(groupPlatform) {
		return ctx
	}
	return context.WithValue(ctx, ctxkey.ForcePlatform, account.Platform)
}
