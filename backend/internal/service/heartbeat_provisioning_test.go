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
