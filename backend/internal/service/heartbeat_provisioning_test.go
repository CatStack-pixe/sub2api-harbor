package service

import (
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestHeartbeatFingerprintValidation(t *testing.T) {
	valid := strings.Repeat("a", 24)
	require.True(t, validHeartbeatFingerprint(valid))
	require.False(t, validHeartbeatFingerprint("not-hex"))
	require.False(t, validHeartbeatFingerprint(strings.Repeat("a", 23)))
	require.True(t, validHeartbeatSessionKey(strings.Repeat("b", 32)))
	require.False(t, validHeartbeatSessionKey("short"))
}

func TestHeartbeatProviderRegistryCoversConfiguredPlatforms(t *testing.T) {
	for _, raw := range []string{"ds", "deepseek", "glm", "zhipu", "kimi", "openai", "toapis", "kling", "anthropic", "gemini", "grok", "nvidia", "tokenrhythm", "modelscope", "dashscope", "minimax", "volcengine", "chatanywhere", "agnes", "antigravity", "sf", "siliconflow", "mimo", "groq", "perplexity", "bailian_sp"} {
		spec, ok := normalizeHeartbeatProvider(raw)
		require.Truef(t, ok, "provider %q should be registered", raw)
		require.NotEmpty(t, spec.ID)
		require.NotEmpty(t, spec.Platform)
	}

	spec, ok := normalizeHeartbeatProvider("deepseek")
	require.True(t, ok)
	require.Equal(t, "ds", spec.ID)
	require.Equal(t, PlatformDeepSeek, spec.Platform)
}

func TestHeartbeatOpenAICompatibleProviderAliases(t *testing.T) {
	for _, tt := range []struct {
		raw string
		id  string
	}{
		{raw: "toapis", id: "toapis"},
		{raw: "kling", id: "kling"},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			spec, ok := normalizeHeartbeatProvider(tt.raw)
			require.True(t, ok)
			require.Equal(t, tt.id, spec.ID)
			require.Equal(t, PlatformOpenAI, spec.Platform)

			platform, ok := HeartbeatProviderPlatform(tt.raw)
			require.True(t, ok)
			require.Equal(t, PlatformOpenAI, platform)
			providerID, ok := HeartbeatProviderID(tt.raw)
			require.True(t, ok)
			require.Equal(t, tt.id, providerID)
		})
	}
}

func TestHeartbeatProviderRegistryRejectsUnknownProvider(t *testing.T) {
	_, ok := normalizeHeartbeatProvider("toapis-unknown")
	require.False(t, ok)
	_, ok = HeartbeatProviderPlatform("kling-unknown")
	require.False(t, ok)
}

func TestHeartbeatProviderAliasesCanonicalizeToAccountPlatform(t *testing.T) {
	platform, ok := HeartbeatProviderPlatform("glm")
	require.True(t, ok)
	require.Equal(t, PlatformGLM, platform)
	platform, ok = HeartbeatProviderPlatform("bigmodel")
	require.True(t, ok)
	require.Equal(t, PlatformZhipu, platform)
	providerID, ok := HeartbeatProviderID("siliconflow")
	require.True(t, ok)
	require.Equal(t, "sf", providerID)
	_, ok = HeartbeatProviderPlatform("unknown-provider")
	require.False(t, ok)
}

func TestHeartbeatAccountCanUseGroupFollowsRoutingCompatibility(t *testing.T) {
	require.True(t, heartbeatAccountCanUseGroup(PlatformComposite, PlatformGLM))
	require.True(t, heartbeatAccountCanUseGroup(PlatformOpenAI, PlatformGLM))
	require.True(t, heartbeatAccountCanUseGroup(PlatformDeepSeek, PlatformOpenAI))
	require.True(t, heartbeatAccountCanUseGroup(PlatformAnthropic, PlatformAnthropic))
	require.False(t, heartbeatAccountCanUseGroup(PlatformAnthropic, PlatformGLM))
}

func TestHeartbeatAccountCredentialsCarriesProviderExtras(t *testing.T) {
	credentials := heartbeatAccountCredentials(PlatformTokenRhythm, &heartbeatVaultCredential{
		Key: "token-key",
		Credentials: map[string]any{
			"tr_session": "session",
			"tr_csrf":    "csrf",
			"base_url":   "https://tokenrhythm.studio",
			"password":   "must-not-be-copied",
		},
	})
	require.Equal(t, "token-key", credentials["api_key"])
	require.Equal(t, "session", credentials["tr_session"])
	require.Equal(t, "csrf", credentials["tr_csrf"])
	require.NotContains(t, credentials, "password")
}

func TestHeartbeatRetryDelayIsBounded(t *testing.T) {
	require.Equal(t, 30*time.Second, heartbeatRetryDelay(1))
	require.Equal(t, time.Duration(1800)*time.Second, heartbeatRetryDelay(99))
}

func TestSampleHeartbeatProxiesKeepsUniqueMembers(t *testing.T) {
	proxies := []Proxy{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}, {ID: 5}}
	sampled, err := sampleHeartbeatProxies(proxies, 3)
	require.NoError(t, err)
	require.Len(t, sampled, 3)
	seen := make(map[int64]struct{}, len(sampled))
	for _, proxy := range sampled {
		_, duplicate := seen[proxy.ID]
		require.False(t, duplicate)
		seen[proxy.ID] = struct{}{}
	}
}

func TestNormalizeHeartbeatConfigKeepsLegacySingleTargetCompatible(t *testing.T) {
	got := normalizeHeartbeatConfig(config.HeartbeatProvisioningConfig{
		DeepSeekGroupID: 12,
		ProxyGroupID:    3,
	})

	require.Equal(t, int64(12), got.DefaultGroupID)
	require.Equal(t, []config.HeartbeatProvisioningTarget{{GroupID: 12, ProxyGroupID: 3}}, got.Targets)
}

func TestResolveHeartbeatTargetUsesDefaultWhenGroupIsOmitted(t *testing.T) {
	cfg := config.HeartbeatProvisioningConfig{
		DefaultGroupID: 12,
		Targets: []config.HeartbeatProvisioningTarget{
			{GroupID: 12, ProxyGroupID: 1},
			{GroupID: 13, ProxyGroupID: 2},
		},
	}

	target, ok := resolveHeartbeatTarget(cfg, nil)
	require.True(t, ok)
	require.Equal(t, config.HeartbeatProvisioningTarget{GroupID: 12, ProxyGroupID: 1}, target)

	groupID := int64(13)
	target, ok = resolveHeartbeatTarget(cfg, &groupID)
	require.True(t, ok)
	require.Equal(t, config.HeartbeatProvisioningTarget{GroupID: 13, ProxyGroupID: 2}, target)

	unknown := int64(99)
	_, ok = resolveHeartbeatTarget(cfg, &unknown)
	require.False(t, ok)
}

func TestResolveHeartbeatJobTargetAllowsUngroupedProxyPool(t *testing.T) {
	cfg := config.HeartbeatProvisioningConfig{
		DefaultGroupID: 12,
		Targets: []config.HeartbeatProvisioningTarget{
			{GroupID: 12, ProxyGroupID: 0},
		},
	}

	target, ok := resolveHeartbeatJobTarget(cfg, &HeartbeatProvisioningJob{TargetGroupID: 12})
	require.True(t, ok)
	require.Equal(t, config.HeartbeatProvisioningTarget{GroupID: 12, ProxyGroupID: 0}, target)
}

func TestValidateHeartbeatRuntimeConfigRequiresDefaultMapping(t *testing.T) {
	cfg := config.HeartbeatProvisioningConfig{
		DefaultGroupID:       12,
		AllowedSourceIPs:     []string{"192.0.2.10"},
		Targets:              []config.HeartbeatProvisioningTarget{{GroupID: 13, ProxyGroupID: 1}},
		WorkerCount:          1,
		ProxyProbeWorkers:    1,
		ProxyProbeSampleSize: 1,
		ProxyProbeTimeoutS:   1,
		ProxySweepTTLSecond:  1,
		MaxAttempts:          1,
	}
	require.ErrorContains(t, validateHeartbeatRuntimeConfig(cfg), "default_group_id")
}

func TestValidateHeartbeatRuntimeConfigAcceptsUngroupedProxyPool(t *testing.T) {
	cfg := config.HeartbeatProvisioningConfig{
		DefaultGroupID:       12,
		AllowedSourceIPs:     []string{"192.0.2.10"},
		Targets:              []config.HeartbeatProvisioningTarget{{GroupID: 12, ProxyGroupID: 0}},
		WorkerCount:          1,
		ProxyProbeWorkers:    1,
		ProxyProbeSampleSize: 1,
		ProxyProbeTimeoutS:   1,
		ProxySweepTTLSecond:  1,
		MaxAttempts:          1,
	}
	require.NoError(t, validateHeartbeatRuntimeConfig(cfg))
}
