package repository

import (
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestRemoteIngestIPPolicyRejectsMappedAndReservedAddresses(t *testing.T) {
	for _, test := range []struct {
		name string
		ip   string
		cidr []string
		ok   bool
	}{
		{name: "public", ip: "8.8.8.8", ok: true},
		{name: "mapped loopback", ip: "::ffff:127.0.0.1", ok: false},
		{name: "mapped private", ip: "::ffff:10.0.0.1", ok: false},
		{name: "IPv4 compatible loopback", ip: "::127.0.0.1", ok: false},
		{name: "IPv4 translated loopback", ip: "::ffff:0:127.0.0.1", ok: false},
		{name: "NAT64 well known", ip: "64:ff9b::7f00:1", ok: false},
		{name: "NAT64 local use", ip: "64:ff9b:1::a00:1", ok: false},
		{name: "Teredo", ip: "2001::1", ok: false},
		{name: "6to4", ip: "2002:7f00:1::", ok: false},
		{name: "documentation", ip: "2001:db8::1", ok: false},
		{name: "link local", ip: "fe80::1", ok: false},
		{name: "explicit allowlist", ip: "10.4.5.6", cidr: []string{"10.0.0.0/8"}, ok: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.ok, remoteIngestIPAllowed(parseRemoteTestAddr(t, test.ip), parseRemoteTestPrefixes(t, test.cidr)))
		})
	}
}

func TestRemoteIngestProbeResponseHasHardLimit(t *testing.T) {
	body := newHardLimitReadCloser(io.NopCloser(strings.NewReader("123456")), 5)
	data, err := io.ReadAll(body)
	require.ErrorIs(t, err, errRemoteIngestResponseTooLarge)
	require.Equal(t, "12345", string(data))
	require.NoError(t, body.Close())
}

func TestRemoteIngestRequestRejectsProxyAndHTTP(t *testing.T) {
	service.RegisterRemoteIngestAccount(987654321)
	request, err := http.NewRequest(http.MethodGet, "http://upstream.example/v1", nil)
	require.NoError(t, err)
	require.Error(t, validateRemoteIngestUpstreamRequest(request, "", 987654321))

	request, err = http.NewRequest(http.MethodGet, "https://upstream.example/v1", nil)
	require.NoError(t, err)
	require.Error(t, validateRemoteIngestUpstreamRequest(request, "http://proxy.example:8080", 987654321))
}

func parseRemoteTestAddr(t *testing.T, raw string) netip.Addr {
	t.Helper()
	addr, err := netip.ParseAddr(raw)
	require.NoError(t, err)
	return addr.Unmap()
}

func parseRemoteTestPrefixes(t *testing.T, raws []string) []netip.Prefix {
	t.Helper()
	prefixes := make([]netip.Prefix, 0, len(raws))
	for _, raw := range raws {
		prefix, err := netip.ParsePrefix(raw)
		require.NoError(t, err)
		prefixes = append(prefixes, prefix)
	}
	return prefixes
}
