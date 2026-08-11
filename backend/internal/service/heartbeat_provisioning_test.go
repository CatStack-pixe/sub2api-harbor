package service

import (
	"strings"
	"testing"
	"time"

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
