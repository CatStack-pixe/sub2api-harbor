package repository

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

func newRemoteIngestDialContext(allowedCIDRs []string) func(context.Context, string, string) (net.Conn, error) {
	allowed := make([]netip.Prefix, 0, len(allowedCIDRs))
	for _, raw := range allowedCIDRs {
		if prefix, err := netip.ParsePrefix(strings.TrimSpace(raw)); err == nil {
			allowed = append(allowed, prefix)
		}
	}
	dialer := newUpstreamDialer()
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || host == "" || port == "" {
			return nil, errors.New("remote ingest upstream address is invalid")
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, fmt.Errorf("resolve remote ingest upstream: %w", err)
		}
		for _, address := range addresses {
			if !remoteIngestIPAllowed(address.Unmap(), allowed) {
				return nil, errors.New("remote ingest upstream resolved to a blocked address")
			}
		}
		var lastErr error
		for _, resolved := range addresses {
			resolved = resolved.Unmap()
			if network == "tcp4" && !resolved.Is4() {
				continue
			}
			if network == "tcp6" && resolved.Is4() {
				continue
			}
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		if lastErr == nil {
			lastErr = errors.New("remote ingest upstream has no address for requested network")
		}
		return nil, lastErr
	}
}

func remoteIngestIPAllowed(ip netip.Addr, allowed []netip.Prefix) bool {
	if !ip.IsValid() {
		return false
	}
	for _, prefix := range allowed {
		if prefix.Contains(ip) {
			return true
		}
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return false
	}
	blocked := []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"100::/64", "2001::/23", "2001:db8::/32", "3fff::/20", "5f00::/16", "fc00::/7", "fe80::/10", "fec0::/10",
	}
	for _, raw := range blocked {
		if netip.MustParsePrefix(raw).Contains(ip) {
			return false
		}
	}
	return ip.IsGlobalUnicast()
}
