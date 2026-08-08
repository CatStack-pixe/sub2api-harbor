package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestValidateRemoteBaseURLStrictSyntax(t *testing.T) {
	normalized, host, err := validateRemoteBaseURL("  https://Upstream.Example:8443/v1/  ")
	require.NoError(t, err)
	require.Equal(t, "https://Upstream.Example:8443/v1", normalized)
	require.Equal(t, "upstream.example", host)

	invalid := []string{
		"",
		"http://upstream.example/v1",
		"//upstream.example/v1",
		"https://user:secret@upstream.example/v1",
		"https://upstream.example/v1?token=value",
		"https://upstream.example/v1?",
		"https://upstream.example/v1#fragment",
		"https://upstream.example:0/v1",
		"https://upstream.example:65536/v1",
		"https:///missing-host",
	}
	for _, raw := range invalid {
		t.Run(raw, func(t *testing.T) {
			_, _, err := validateRemoteBaseURL(raw)
			require.Error(t, err)
		})
	}
}

func TestRemoteIngestResolvedHostRejectsSSRFAddresses(t *testing.T) {
	resolver := &remoteHostResolverStub{addresses: map[string][]netip.Addr{
		"public.example":   {netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("2606:4700:4700::1111")},
		"mixed.example":    {netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")},
		"private.example":  {netip.MustParseAddr("10.10.1.4")},
		"reserved.example": {netip.MustParseAddr("198.51.100.8")},
	}}
	service := &RemoteIngestService{resolver: resolver}

	require.NoError(t, service.validateResolvedHost(context.Background(), "public.example"))

	blocked := []string{
		"localhost",
		"metadata.localhost",
		"127.0.0.1",
		"::1",
		"::ffff:127.0.0.1",
		"10.0.0.1",
		"169.254.169.254",
		"100.100.100.200",
		"192.0.2.1",
		"198.51.100.8",
		"203.0.113.1",
		"224.0.0.1",
		"::127.0.0.1",
		"::ffff:0:127.0.0.1",
		"64:ff9b::7f00:1",
		"64:ff9b:1::a00:1",
		"2001::1",
		"2002:7f00:1::",
		"2001:db8::1",
		"mixed.example",
		"private.example",
		"reserved.example",
		"missing.example",
	}
	for _, host := range blocked {
		t.Run(host, func(t *testing.T) {
			require.Error(t, service.validateResolvedHost(context.Background(), host))
		})
	}
}

func TestRemoteIngestResolvedHostAllowsExplicitPrivateCIDR(t *testing.T) {
	resolver := &remoteHostResolverStub{addresses: map[string][]netip.Addr{
		"private.example": {netip.MustParseAddr("10.10.1.4")},
	}}
	service := &RemoteIngestService{
		resolver:       resolver,
		allowedPrivate: []netip.Prefix{netip.MustParsePrefix("10.10.0.0/16")},
	}
	require.NoError(t, service.validateResolvedHost(context.Background(), "private.example"))
	require.NoError(t, service.validateResolvedHost(context.Background(), "10.10.1.4"))
	require.Error(t, service.validateResolvedHost(context.Background(), "10.11.1.4"))
}

func TestSanitizeRemoteProbeErrorNeverPersistsUpstreamBody(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "HTTP status without response body",
			raw:  `API returned 401: {"error":"reflected-secret"}`,
			want: "upstream probe failed with HTTP 401",
		},
		{
			name: "provider-specific HTTP status",
			raw:  `Grok Responses API returned 429: attacker-controlled-body`,
			want: "upstream probe failed with HTTP 429",
		},
		{
			name: "connection details",
			raw:  "Request failed: dial tcp 192.0.2.2:443: reflected-secret",
			want: "upstream connection failed",
		},
		{
			name: "timeout",
			raw:  "probe timed out",
			want: "probe timed out",
		},
		{
			name: "arbitrary response",
			raw:  `{"api_key":"reflected-secret"}`,
			want: "upstream probe failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeRemoteProbeError(tt.raw)
			require.Equal(t, tt.want, got)
			require.NotContains(t, got, "reflected-secret")
		})
	}
}

func TestRemoteIngestSubmitRejectsTamperingWithoutConsumingChallengeAndRejectsReplay(t *testing.T) {
	harness := newRemoteSubmitHarness(t)
	body := harness.validBody(t, "original-key")
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := harness.sign(t, body, timestamp)

	tamperedBody := harness.validBody(t, "tampered-key")
	_, _, err := harness.service.Submit(
		context.Background(), harness.client.ID, harness.challengeID, timestamp,
		signature, tamperedBody, harness.client.AccessSubject,
	)
	require.ErrorIs(t, err, ErrRemoteSignatureInvalid)
	require.False(t, harness.challenges.consumed)
	require.Zero(t, harness.repo.createCalls)

	delivery, queryToken, err := harness.service.Submit(
		context.Background(), harness.client.ID, harness.challengeID, timestamp,
		signature, body, harness.client.AccessSubject,
	)
	require.NoError(t, err)
	require.NotNil(t, delivery)
	require.NotEmpty(t, queryToken)
	require.True(t, harness.challenges.consumed)
	require.Equal(t, 1, harness.repo.createCalls)
	require.Equal(t, "encrypted:original-key", harness.repo.lastCreate.EncryptedAPIKey)
	require.Equal(t, sha256Bytes(body), harness.repo.lastCreate.PayloadHash)

	_, _, err = harness.service.Submit(
		context.Background(), harness.client.ID, harness.challengeID, timestamp,
		signature, body, harness.client.AccessSubject,
	)
	require.ErrorIs(t, err, ErrRemoteChallengeInvalid)
	require.Equal(t, 1, harness.repo.createCalls)
}

func TestRemoteIngestSubmitRequiresFreshUnixTimestampAndBoundSignature(t *testing.T) {
	tests := []struct {
		name      string
		timestamp func() string
		signFor   func(timestamp string) string
		want      error
	}{
		{
			name:      "RFC3339 rejected",
			timestamp: func() string { return time.Now().UTC().Format(time.RFC3339) },
			want:      ErrRemoteSignatureInvalid,
		},
		{
			name:      "stale timestamp rejected",
			timestamp: func() string { return strconv.FormatInt(time.Now().Add(-2*time.Minute).Unix(), 10) },
			want:      ErrRemoteSignatureInvalid,
		},
		{
			name:      "future timestamp rejected",
			timestamp: func() string { return strconv.FormatInt(time.Now().Add(2*time.Minute).Unix(), 10) },
			want:      ErrRemoteSignatureInvalid,
		},
		{
			name:      "timestamp alteration invalidates signature",
			timestamp: func() string { return strconv.FormatInt(time.Now().Unix(), 10) },
			signFor: func(timestamp string) string {
				unix, err := strconv.ParseInt(timestamp, 10, 64)
				require.NoError(t, err)
				return strconv.FormatInt(unix-1, 10)
			},
			want: ErrRemoteSignatureInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newRemoteSubmitHarness(t)
			body := harness.validBody(t, "api-key")
			timestamp := tt.timestamp()
			signedTimestamp := timestamp
			if tt.signFor != nil {
				signedTimestamp = tt.signFor(timestamp)
			}
			signature := harness.sign(t, body, signedTimestamp)
			_, _, err := harness.service.Submit(
				context.Background(), harness.client.ID, harness.challengeID, timestamp,
				signature, body, harness.client.AccessSubject,
			)
			require.ErrorIs(t, err, tt.want)
			require.False(t, harness.challenges.consumed)
			require.Zero(t, harness.repo.createCalls)
		})
	}
}

type remoteSubmitHarness struct {
	service     *RemoteIngestService
	repo        *remoteIngestRepositoryStub
	challenges  *remoteChallengeStoreStub
	client      *RemoteClient
	privateKey  ed25519.PrivateKey
	challengeID string
}

func newRemoteSubmitHarness(t *testing.T) *remoteSubmitHarness {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	client := &RemoteClient{
		ID:            "client-1",
		PublicKey:     publicKey,
		AccessSubject: "service-token-1",
	}
	repo := &remoteIngestRepositoryStub{client: client}
	challenges := &remoteChallengeStoreStub{challengeID: "challenge-1", nonce: "nonce-1"}
	cfg := &config.Config{RemoteIngest: config.RemoteIngestConfig{
		Enabled:       true,
		TimestampSkew: 60,
	}}
	service := NewRemoteIngestService(repo, challenges, remoteAccessVerifierStub{}, remoteCipherStub{}, nil, cfg)
	service.resolver = &remoteHostResolverStub{addresses: map[string][]netip.Addr{
		"upstream.example": {netip.MustParseAddr("8.8.8.8")},
	}}
	return &remoteSubmitHarness{
		service: service, repo: repo, challenges: challenges, client: client,
		privateKey: privateKey, challengeID: challenges.challengeID,
	}
}

func (h *remoteSubmitHarness) validBody(t *testing.T, apiKey string) []byte {
	t.Helper()
	body, err := json.Marshal(RemoteAccountSubmission{
		ExternalID:  "remote-account-001",
		Name:        "upstream-account-001",
		Platform:    PlatformOpenAI,
		BaseURL:     "https://upstream.example/v1",
		APIKey:      apiKey,
		GroupName:   "openai-default",
		TestModel:   "gpt-4.1-mini",
		Concurrency: 1,
	})
	require.NoError(t, err)
	return body
}

func (h *remoteSubmitHarness) sign(t *testing.T, body []byte, timestamp string) string {
	t.Helper()
	hash := sha256.Sum256(body)
	payload := RemoteSigningPayload(h.client.ID, h.challengeID, h.challenges.nonce, timestamp, hex.EncodeToString(hash[:]))
	return base64.StdEncoding.EncodeToString(ed25519.Sign(h.privateKey, []byte(payload)))
}

func sha256Bytes(value []byte) []byte {
	hash := sha256.Sum256(value)
	return hash[:]
}

type remoteHostResolverStub struct {
	addresses map[string][]netip.Addr
}

func (r *remoteHostResolverStub) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	addresses := r.addresses[host]
	if len(addresses) == 0 {
		return nil, errors.New("host not found")
	}
	return addresses, nil
}

type remoteChallengeStoreStub struct {
	challengeID string
	nonce       string
	consumed    bool
}

func (s *remoteChallengeStoreStub) Create(_ context.Context, _ string, _ time.Duration) (*RemoteChallenge, error) {
	return &RemoteChallenge{ID: s.challengeID, Nonce: s.nonce, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (s *remoteChallengeStoreStub) Get(_ context.Context, _, challengeID string) (string, error) {
	if s.consumed || challengeID != s.challengeID {
		return "", ErrRemoteChallengeInvalid
	}
	return s.nonce, nil
}

func (s *remoteChallengeStoreStub) Consume(_ context.Context, _, challengeID, nonce string) (bool, error) {
	if s.consumed || challengeID != s.challengeID || nonce != s.nonce {
		return false, nil
	}
	s.consumed = true
	return true, nil
}

type remoteAccessVerifierStub struct{}

func (remoteAccessVerifierStub) Verify(_ context.Context, _ string) (*CloudflareAccessIdentity, error) {
	return &CloudflareAccessIdentity{Subject: "service-token-1"}, nil
}

type remoteCipherStub struct{}

func (remoteCipherStub) EncryptString(value string) (string, error) {
	return "encrypted:" + value, nil
}

func (remoteCipherStub) DecryptString(value string) (string, error) {
	return value, nil
}

type remoteIngestRepositoryStub struct {
	client      *RemoteClient
	createCalls int
	lastCreate  RemoteDeliveryCreate
}

func (r *remoteIngestRepositoryStub) CreateRegistrationToken(context.Context, *RemoteRegistrationToken, []byte) error {
	return nil
}

func (r *remoteIngestRepositoryStub) ListRegistrationTokens(context.Context, int) ([]RemoteRegistrationToken, error) {
	return nil, nil
}

func (r *remoteIngestRepositoryStub) ConsumeRegistrationToken(context.Context, []byte, *RemoteClient) error {
	return nil
}

func (r *remoteIngestRepositoryStub) GetClient(_ context.Context, id string) (*RemoteClient, error) {
	if r.client == nil || id != r.client.ID {
		return nil, ErrRemoteClientUnauthorized
	}
	copy := *r.client
	return &copy, nil
}

func (r *remoteIngestRepositoryStub) ListClients(context.Context, int) ([]RemoteClient, error) {
	return nil, nil
}

func (r *remoteIngestRepositoryStub) RevokeClient(context.Context, string) error { return nil }
func (r *remoteIngestRepositoryStub) TouchClient(context.Context, string) error  { return nil }

func (r *remoteIngestRepositoryStub) CreateDelivery(_ context.Context, create RemoteDeliveryCreate) (*RemoteDelivery, bool, error) {
	r.createCalls++
	r.lastCreate = create
	return &RemoteDelivery{
		ID:         create.ID,
		ClientID:   create.ClientID,
		ExternalID: create.Submission.ExternalID,
		Status:     RemoteDeliveryPending,
	}, true, nil
}

func (r *remoteIngestRepositoryStub) GetDelivery(context.Context, string, []byte) (*RemoteDelivery, error) {
	return nil, ErrRemoteDeliveryNotFound
}

func (r *remoteIngestRepositoryStub) ListDeliveries(context.Context, string, int) ([]RemoteDelivery, error) {
	return nil, nil
}

func (r *remoteIngestRepositoryStub) ClaimProbe(context.Context, time.Duration) (*RemoteProbeJob, error) {
	return nil, nil
}

func (r *remoteIngestRepositoryStub) CompleteProbe(context.Context, string, int) error { return nil }
func (r *remoteIngestRepositoryStub) FailProbe(context.Context, string, int, string) error { return nil }
func (r *remoteIngestRepositoryStub) RetryProbe(context.Context, string) error        { return nil }
