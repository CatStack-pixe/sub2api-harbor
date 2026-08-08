package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/google/uuid"
)

const (
	RemoteIngestProtocolVersion = "sub2api-remote-ingest-v1"
	RemoteDeliveryPending       = "pending"
	RemoteDeliveryProbing       = "probing"
	RemoteDeliveryActive        = "active"
	RemoteDeliveryProbeFailed   = "probe_failed"
)

var (
	ErrRemoteIngestDisabled     = infraerrors.NotFound("REMOTE_INGEST_DISABLED", "remote ingest is disabled")
	ErrRemoteAccessUnauthorized = infraerrors.Unauthorized("REMOTE_ACCESS_UNAUTHORIZED", "Cloudflare Access authentication failed")
	ErrRemoteClientUnauthorized = infraerrors.Unauthorized("REMOTE_CLIENT_UNAUTHORIZED", "remote client authentication failed")
	ErrRemoteClientRevoked      = infraerrors.Forbidden("REMOTE_CLIENT_REVOKED", "remote client is revoked")
	ErrRemoteClientConflict     = infraerrors.Conflict("REMOTE_CLIENT_CONFLICT", "Cloudflare Access identity or public key is already enrolled")
	ErrRemoteTokenInvalid       = infraerrors.Unauthorized("REMOTE_ENROLLMENT_TOKEN_INVALID", "registration token is invalid or expired")
	ErrRemoteChallengeInvalid   = infraerrors.Unauthorized("REMOTE_CHALLENGE_INVALID", "challenge is invalid or expired")
	ErrRemoteSignatureInvalid   = infraerrors.Unauthorized("REMOTE_SIGNATURE_INVALID", "request signature is invalid")
	ErrRemoteDeliveryConflict   = infraerrors.Conflict("REMOTE_DELIVERY_CONFLICT", "external_id already exists with different content")
	ErrRemoteDeliveryNotFound   = infraerrors.NotFound("REMOTE_DELIVERY_NOT_FOUND", "delivery not found")
	ErrRemoteGroupInvalid       = infraerrors.BadRequest("REMOTE_GROUP_INVALID", "group is not available for this platform")
	ErrRemoteBaseURLInvalid     = infraerrors.BadRequest("REMOTE_BASE_URL_INVALID", "base_url is not allowed")
)

var remoteIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type RemoteRegistrationToken struct {
	ID          string     `json:"id"`
	Token       string     `json:"token,omitempty"`
	Fingerprint string     `json:"fingerprint"`
	ExpiresAt   time.Time  `json:"expires_at"`
	UsedAt      *time.Time `json:"used_at,omitempty"`
	ClientID    *string    `json:"client_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type RemoteClient struct {
	ID                   string     `json:"id"`
	MachineName          string     `json:"machine_name"`
	PublicKey            []byte     `json:"-"`
	PublicKeyFingerprint string     `json:"public_key_fingerprint"`
	AccessSubject        string     `json:"access_subject"`
	EnrolledAt           time.Time  `json:"enrolled_at"`
	LastActiveAt         *time.Time `json:"last_active_at,omitempty"`
	RevokedAt            *time.Time `json:"revoked_at,omitempty"`
}

type RemoteDelivery struct {
	ID          string     `json:"id"`
	ClientID    string     `json:"client_id"`
	ExternalID  string     `json:"external_id"`
	AccountID   int64      `json:"account_id"`
	Platform    string     `json:"platform"`
	GroupName   string     `json:"group_name"`
	TestModel   string     `json:"test_model,omitempty"`
	Status      string     `json:"status"`
	MaskedError string     `json:"masked_error,omitempty"`
	Attempts    int        `json:"attempts"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	QueryCipher string     `json:"-"`
}

type RemoteAccountSubmission struct {
	ExternalID    string  `json:"external_id"`
	Name          string  `json:"name"`
	Platform      string  `json:"platform"`
	BaseURL       string  `json:"base_url"`
	APIKey        string  `json:"api_key"`
	GroupName     string  `json:"group_name"`
	TestModel     string  `json:"test_model"`
	Concurrency   int     `json:"concurrency"`
	Priority      int     `json:"priority"`
	RateMultiplier float64 `json:"rate_multiplier"`
}

type RemoteDeliveryCreate struct {
	ID                   string
	ClientID             string
	Submission           RemoteAccountSubmission
	PayloadHash          []byte
	EncryptedAPIKey      string
	QueryTokenHash       []byte
	QueryTokenCiphertext string
}

type RemoteProbeJob struct {
	DeliveryID string
	AccountID  int64
	TestModel  string
	Attempts   int
}

type RemoteIngestRepository interface {
	CreateRegistrationToken(ctx context.Context, token *RemoteRegistrationToken, tokenHash []byte) error
	ListRegistrationTokens(ctx context.Context, limit int) ([]RemoteRegistrationToken, error)
	ConsumeRegistrationToken(ctx context.Context, tokenHash []byte, client *RemoteClient) error
	GetClient(ctx context.Context, id string) (*RemoteClient, error)
	ListClients(ctx context.Context, limit int) ([]RemoteClient, error)
	RevokeClient(ctx context.Context, id string) error
	TouchClient(ctx context.Context, id string) error
	CreateDelivery(ctx context.Context, create RemoteDeliveryCreate) (*RemoteDelivery, bool, error)
	GetDelivery(ctx context.Context, id string, queryTokenHash []byte) (*RemoteDelivery, error)
	ListDeliveries(ctx context.Context, status string, limit int) ([]RemoteDelivery, error)
	ClaimProbe(ctx context.Context, lease time.Duration) (*RemoteProbeJob, error)
	CompleteProbe(ctx context.Context, deliveryID string, attempt int) error
	FailProbe(ctx context.Context, deliveryID string, attempt int, maskedError string) error
	RetryProbe(ctx context.Context, deliveryID string) error
}

type RemoteChallenge struct {
	ID        string    `json:"challenge_id"`
	Nonce     string    `json:"nonce"`
	ExpiresAt time.Time `json:"expires_at"`
}

type RemoteChallengeStore interface {
	Create(ctx context.Context, clientID string, ttl time.Duration) (*RemoteChallenge, error)
	Get(ctx context.Context, clientID, challengeID string) (string, error)
	Consume(ctx context.Context, clientID, challengeID, nonce string) (bool, error)
}

type RemoteAccessVerifier interface {
	Verify(ctx context.Context, assertion string) (*CloudflareAccessIdentity, error)
}

type RemoteCredentialCipher interface {
	EncryptString(plaintext string) (string, error)
	DecryptString(ciphertext string) (string, error)
}

type RemoteHostResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type RemoteIngestService struct {
	repo       RemoteIngestRepository
	challenges RemoteChallengeStore
	access     RemoteAccessVerifier
	cipher     RemoteCredentialCipher
	accountTest *AccountTestService
	cfg        *config.Config
	resolver   RemoteHostResolver
	allowedPrivate []netip.Prefix

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func NewRemoteIngestService(
	repo RemoteIngestRepository,
	challenges RemoteChallengeStore,
	access RemoteAccessVerifier,
	cipher RemoteCredentialCipher,
	accountTest *AccountTestService,
	cfg *config.Config,
) *RemoteIngestService {
	s := &RemoteIngestService{
		repo: repo, challenges: challenges, access: access, cipher: cipher,
		accountTest: accountTest, cfg: cfg, resolver: net.DefaultResolver,
		stopCh: make(chan struct{}),
	}
	if cfg != nil {
		for _, raw := range cfg.RemoteIngest.AllowedPrivateCIDRs {
			if prefix, err := netip.ParsePrefix(raw); err == nil {
				s.allowedPrivate = append(s.allowedPrivate, prefix)
			}
		}
	}
	return s
}

func (s *RemoteIngestService) Enabled() bool {
	return s != nil && s.cfg != nil && s.cfg.RemoteIngest.Enabled
}

func (s *RemoteIngestService) VerifyAccess(ctx context.Context, assertion string) (string, error) {
	if !s.Enabled() {
		return "", ErrRemoteIngestDisabled
	}
	if s.access == nil {
		return "", ErrRemoteAccessUnauthorized
	}
	identity, err := s.access.Verify(ctx, strings.TrimSpace(assertion))
	if err != nil || identity == nil || strings.TrimSpace(identity.Subject) == "" {
		return "", ErrRemoteAccessUnauthorized
	}
	return identity.Subject, nil
}

func (s *RemoteIngestService) CreateRegistrationToken(ctx context.Context, ttl time.Duration) (*RemoteRegistrationToken, error) {
	if !s.Enabled() {
		return nil, ErrRemoteIngestDisabled
	}
	if ttl <= 0 {
		ttl = time.Duration(s.cfg.RemoteIngest.RegistrationTTL) * time.Second
	}
	if ttl > 24*time.Hour {
		return nil, infraerrors.BadRequest("REMOTE_TOKEN_TTL_INVALID", "registration token TTL cannot exceed 24 hours")
	}
	plain, err := randomRemoteToken()
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(plain))
	now := time.Now().UTC()
	token := &RemoteRegistrationToken{
		ID: uuid.NewString(), Token: plain, Fingerprint: hex.EncodeToString(hash[:8]),
		ExpiresAt: now.Add(ttl), CreatedAt: now,
	}
	if err := s.repo.CreateRegistrationToken(ctx, token, hash[:]); err != nil {
		return nil, err
	}
	return token, nil
}

func (s *RemoteIngestService) ProtectOneTimeSecret(value string) (string, error) {
	if s == nil || s.cipher == nil {
		return "", ErrRemoteIngestKeyringDisabled
	}
	return s.cipher.EncryptString(value)
}

func (s *RemoteIngestService) RevealOneTimeSecret(value string) (string, error) {
	if s == nil || s.cipher == nil {
		return "", ErrRemoteIngestKeyringDisabled
	}
	return s.cipher.DecryptString(value)
}

func (s *RemoteIngestService) ListRegistrationTokens(ctx context.Context, limit int) ([]RemoteRegistrationToken, error) {
	if !s.Enabled() { return nil, ErrRemoteIngestDisabled }
	return s.repo.ListRegistrationTokens(ctx, boundedRemoteLimit(limit))
}

func (s *RemoteIngestService) Enroll(ctx context.Context, registrationToken, machineName, publicKeyBase64, accessSubject string) (*RemoteClient, error) {
	if !s.Enabled() { return nil, ErrRemoteIngestDisabled }
	machineName = strings.TrimSpace(machineName)
	if machineName == "" || len(machineName) > 100 {
		return nil, infraerrors.BadRequest("REMOTE_MACHINE_NAME_INVALID", "machine_name must be between 1 and 100 characters")
	}
	publicKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKeyBase64))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return nil, infraerrors.BadRequest("REMOTE_PUBLIC_KEY_INVALID", "public_key must be a Base64 Ed25519 public key")
	}
	registrationToken = strings.TrimSpace(registrationToken)
	if registrationToken == "" { return nil, ErrRemoteTokenInvalid }
	tokenHash := sha256.Sum256([]byte(registrationToken))
	keyHash := sha256.Sum256(publicKey)
	client := &RemoteClient{
		ID: uuid.NewString(), MachineName: machineName, PublicKey: publicKey,
		PublicKeyFingerprint: hex.EncodeToString(keyHash[:]), AccessSubject: strings.TrimSpace(accessSubject),
		EnrolledAt: time.Now().UTC(),
	}
	if client.AccessSubject == "" { return nil, ErrRemoteAccessUnauthorized }
	if err := s.repo.ConsumeRegistrationToken(ctx, tokenHash[:], client); err != nil {
		if errors.Is(err, ErrRemoteTokenInvalid) { return nil, err }
		return nil, err
	}
	return client, nil
}

func (s *RemoteIngestService) Handshake(ctx context.Context, clientID, accessSubject string) (*RemoteChallenge, error) {
	client, err := s.authorizeClient(ctx, clientID, accessSubject)
	if err != nil { return nil, err }
	challenge, err := s.challenges.Create(ctx, client.ID, time.Duration(s.cfg.RemoteIngest.ChallengeTTL)*time.Second)
	if err == nil { _ = s.repo.TouchClient(ctx, client.ID) }
	return challenge, err
}

func (s *RemoteIngestService) Submit(ctx context.Context, clientID, challengeID, timestamp, signatureB64 string, rawBody []byte, accessSubject string) (*RemoteDelivery, string, error) {
	client, err := s.authorizeClient(ctx, clientID, accessSubject)
	if err != nil { return nil, "", err }
	if len(rawBody) == 0 || len(rawBody) > 16*1024 {
		return nil, "", infraerrors.BadRequest("REMOTE_BODY_INVALID", "request body must be between 1 and 16384 bytes")
	}
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || unix <= 0 {
		return nil, "", ErrRemoteSignatureInvalid
	}
	ts := time.Unix(unix, 0)
	if delta := time.Since(ts); delta > time.Duration(s.cfg.RemoteIngest.TimestampSkew)*time.Second || delta < -time.Duration(s.cfg.RemoteIngest.TimestampSkew)*time.Second {
		return nil, "", ErrRemoteSignatureInvalid
	}
	nonce, err := s.challenges.Get(ctx, client.ID, strings.TrimSpace(challengeID))
	if err != nil || nonce == "" { return nil, "", ErrRemoteChallengeInvalid }
	bodyHash := sha256.Sum256(rawBody)
	canonical := RemoteSigningPayload(client.ID, challengeID, nonce, timestamp, hex.EncodeToString(bodyHash[:]))
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signatureB64))
	if err != nil || !ed25519.Verify(ed25519.PublicKey(client.PublicKey), []byte(canonical), signature) {
		return nil, "", ErrRemoteSignatureInvalid
	}
	consumed, err := s.challenges.Consume(ctx, client.ID, challengeID, nonce)
	if err != nil { return nil, "", err }
	if !consumed { return nil, "", ErrRemoteChallengeInvalid }

	var submission RemoteAccountSubmission
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&submission); err != nil {
		return nil, "", infraerrors.BadRequest("REMOTE_PAYLOAD_INVALID", "request body is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, "", infraerrors.BadRequest("REMOTE_PAYLOAD_INVALID", "request body is invalid")
	}
	if err := s.validateSubmission(ctx, &submission); err != nil { return nil, "", err }
	encryptedAPIKey, err := s.cipher.EncryptString(submission.APIKey)
	if err != nil { return nil, "", fmt.Errorf("encrypt remote credential: %w", err) }
	queryToken, err := randomRemoteToken()
	if err != nil { return nil, "", err }
	queryHash := sha256.Sum256([]byte(queryToken))
	queryCipher, err := s.cipher.EncryptString(queryToken)
	if err != nil { return nil, "", fmt.Errorf("encrypt delivery query token: %w", err) }
	delivery, created, err := s.repo.CreateDelivery(ctx, RemoteDeliveryCreate{
		ID: uuid.NewString(), ClientID: client.ID, Submission: submission, PayloadHash: bodyHash[:],
		EncryptedAPIKey: encryptedAPIKey, QueryTokenHash: queryHash[:], QueryTokenCiphertext: queryCipher,
	})
	if err != nil { return nil, "", err }
	if !created {
		queryToken, err = s.cipher.DecryptString(delivery.QueryCipher)
		if err != nil { return nil, "", fmt.Errorf("decrypt delivery query token: %w", err) }
	}
	_ = s.repo.TouchClient(ctx, client.ID)
	return delivery, queryToken, nil
}

func RemoteSigningPayload(clientID, challengeID, nonce, timestamp, bodyHashHex string) string {
	return strings.Join([]string{RemoteIngestProtocolVersion, clientID, challengeID, nonce, timestamp, strings.ToLower(bodyHashHex)}, "\n")
}

func (s *RemoteIngestService) GetDelivery(ctx context.Context, id, queryToken string) (*RemoteDelivery, error) {
	if !s.Enabled() { return nil, ErrRemoteIngestDisabled }
	hash := sha256.Sum256([]byte(strings.TrimSpace(queryToken)))
	return s.repo.GetDelivery(ctx, strings.TrimSpace(id), hash[:])
}

func (s *RemoteIngestService) GetDeliveryAuthorized(ctx context.Context, id, queryToken, accessSubject string) (*RemoteDelivery, error) {
	delivery, err := s.GetDelivery(ctx, id, queryToken)
	if err != nil { return nil, err }
	if _, err := s.authorizeClient(ctx, delivery.ClientID, accessSubject); err != nil { return nil, err }
	return delivery, nil
}

func (s *RemoteIngestService) ListClients(ctx context.Context, limit int) ([]RemoteClient, error) {
	if !s.Enabled() { return nil, ErrRemoteIngestDisabled }
	return s.repo.ListClients(ctx, boundedRemoteLimit(limit))
}

func (s *RemoteIngestService) RevokeClient(ctx context.Context, id string) error {
	if !s.Enabled() { return ErrRemoteIngestDisabled }
	return s.repo.RevokeClient(ctx, strings.TrimSpace(id))
}

func (s *RemoteIngestService) ListDeliveries(ctx context.Context, status string, limit int) ([]RemoteDelivery, error) {
	if !s.Enabled() { return nil, ErrRemoteIngestDisabled }
	status = strings.TrimSpace(status)
	if status != "" && status != RemoteDeliveryPending && status != RemoteDeliveryProbing && status != RemoteDeliveryActive && status != RemoteDeliveryProbeFailed {
		return nil, infraerrors.BadRequest("REMOTE_DELIVERY_STATUS_INVALID", "invalid delivery status")
	}
	return s.repo.ListDeliveries(ctx, status, boundedRemoteLimit(limit))
}

func (s *RemoteIngestService) RetryDelivery(ctx context.Context, id string) error {
	if !s.Enabled() { return ErrRemoteIngestDisabled }
	return s.repo.RetryProbe(ctx, strings.TrimSpace(id))
}

func (s *RemoteIngestService) authorizeClient(ctx context.Context, clientID, accessSubject string) (*RemoteClient, error) {
	if !s.Enabled() { return nil, ErrRemoteIngestDisabled }
	client, err := s.repo.GetClient(ctx, strings.TrimSpace(clientID))
	if err != nil { return nil, ErrRemoteClientUnauthorized }
	if client.RevokedAt != nil { return nil, ErrRemoteClientRevoked }
	if client.AccessSubject == "" || client.AccessSubject != strings.TrimSpace(accessSubject) {
		return nil, ErrRemoteClientUnauthorized
	}
	return client, nil
}

func (s *RemoteIngestService) validateSubmission(ctx context.Context, in *RemoteAccountSubmission) error {
	in.ExternalID = strings.TrimSpace(in.ExternalID)
	in.Name = strings.TrimSpace(in.Name)
	in.Platform = strings.ToLower(strings.TrimSpace(in.Platform))
	in.GroupName = strings.TrimSpace(in.GroupName)
	in.TestModel = strings.TrimSpace(in.TestModel)
	in.APIKey = strings.TrimSpace(in.APIKey)
	if !remoteIdentifierPattern.MatchString(in.ExternalID) { return infraerrors.BadRequest("REMOTE_EXTERNAL_ID_INVALID", "external_id is invalid") }
	if in.Name == "" || len(in.Name) > 100 { return infraerrors.BadRequest("REMOTE_ACCOUNT_NAME_INVALID", "name must be between 1 and 100 characters") }
	if in.APIKey == "" || len(in.APIKey) > 8192 { return infraerrors.BadRequest("REMOTE_API_KEY_INVALID", "api_key is invalid") }
	if in.GroupName == "" || len(in.GroupName) > 100 { return ErrRemoteGroupInvalid }
	if in.TestModel == "" || len(in.TestModel) > 200 { return infraerrors.BadRequest("REMOTE_TEST_MODEL_INVALID", "test_model is required and must be at most 200 characters") }
	switch in.Platform {
	case PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformGrok, PlatformAgnes, PlatformDeepSeek, PlatformNvidia:
	default:
		return infraerrors.BadRequest("REMOTE_PLATFORM_INVALID", "platform is not supported")
	}
	if in.Concurrency < 1 || in.Concurrency > 1000 { return infraerrors.BadRequest("REMOTE_CONCURRENCY_INVALID", "concurrency must be between 1 and 1000") }
	if in.Priority < 0 || in.Priority > 100000 { return infraerrors.BadRequest("REMOTE_PRIORITY_INVALID", "priority must be between 0 and 100000") }
	if in.RateMultiplier < 0 || in.RateMultiplier > 1000 { return infraerrors.BadRequest("REMOTE_RATE_MULTIPLIER_INVALID", "rate_multiplier must be between 0 and 1000") }
	normalized, host, err := validateRemoteBaseURL(in.BaseURL)
	if err != nil { return ErrRemoteBaseURLInvalid }
	if err := s.validateResolvedHost(ctx, host); err != nil { return ErrRemoteBaseURLInvalid }
	in.BaseURL = normalized
	return nil
}

func validateRemoteBaseURL(raw string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" {
		return "", "", ErrRemoteBaseURLInvalid
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", "", ErrRemoteBaseURLInvalid
	}
	if parsed.Opaque != "" || strings.Contains(parsed.Host, "@") { return "", "", ErrRemoteBaseURLInvalid }
	if port := parsed.Port(); port != "" {
		var value int
		if _, err := fmt.Sscan(port, &value); err != nil || value < 1 || value > 65535 { return "", "", ErrRemoteBaseURLInvalid }
	}
	parsed.RawPath = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/"), strings.ToLower(parsed.Hostname()), nil
}

func (s *RemoteIngestService) validateResolvedHost(ctx context.Context, host string) error {
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		if s.isAllowedRemoteIP(ip) { return nil }
		return ErrRemoteBaseURLInvalid
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") { return ErrRemoteBaseURLInvalid }
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addresses, err := s.resolver.LookupNetIP(lookupCtx, "ip", host)
	if err != nil || len(addresses) == 0 { return ErrRemoteBaseURLInvalid }
	for _, ip := range addresses {
		if !s.isAllowedRemoteIP(ip.Unmap()) { return ErrRemoteBaseURLInvalid }
	}
	return nil
}

func (s *RemoteIngestService) isAllowedRemoteIP(ip netip.Addr) bool {
	for _, prefix := range s.allowedPrivate { if prefix.Contains(ip) { return true } }
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsMulticast() { return false }
	blocked := []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"100::/64", "2001::/23", "2001:db8::/32", "3fff::/20", "5f00::/16", "fc00::/7", "fe80::/10", "fec0::/10",
	}
	for _, raw := range blocked { prefix := netip.MustParsePrefix(raw); if prefix.Contains(ip) { return false } }
	return ip.IsGlobalUnicast()
}

func (s *RemoteIngestService) Start() {
	if !s.Enabled() { return }
	count := s.cfg.RemoteIngest.WorkerConcurrency
	for i := 0; i < count; i++ {
		s.wg.Add(1)
		go s.workerLoop()
	}
}

func (s *RemoteIngestService) Stop() {
	if s == nil { return }
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

func (s *RemoteIngestService) workerLoop() {
	defer s.wg.Done()
	poll := time.NewTicker(time.Duration(s.cfg.RemoteIngest.WorkerPollInterval) * time.Second)
	defer poll.Stop()
	for {
		select {
		case <-s.stopCh: return
		case <-poll.C:
			for s.runOneProbe() {}
		}
	}
}

func (s *RemoteIngestService) runOneProbe() bool {
	lease := time.Duration(s.cfg.RemoteIngest.WorkerTimeout) * time.Second
	job, err := s.repo.ClaimProbe(context.Background(), lease)
	if err != nil || job == nil { return false }
	if s.accountTest == nil {
		_ = s.repo.FailProbe(context.Background(), job.DeliveryID, job.Attempts, "probe service is unavailable")
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), lease)
	result, probeErr := s.accountTest.RunTestBackground(ctx, job.AccountID, job.TestModel)
	contextErr := ctx.Err()
	cancel()
	if probeErr == nil && contextErr == nil && result != nil && result.Status == "success" {
		if err := s.repo.CompleteProbe(context.Background(), job.DeliveryID, job.Attempts); err != nil {
			_ = s.repo.FailProbe(context.Background(), job.DeliveryID, job.Attempts, "activation policy changed during probe")
		}
		return true
	}
	message := "probe failed"
	if result != nil && strings.TrimSpace(result.ErrorMessage) != "" { message = result.ErrorMessage }
	if probeErr != nil { message = probeErr.Error() }
	if contextErr != nil { message = "probe timed out" }
	message = s.accountTest.RedactAccountSecrets(context.Background(), job.AccountID, message)
	message = sanitizeRemoteProbeError(message)
	_ = s.repo.FailProbe(context.Background(), job.DeliveryID, job.Attempts, message)
	return true
}

func sanitizeRemoteProbeError(value string) string {
	value = logredact.RedactText(strings.TrimSpace(value))
	if len(value) > 512 { value = truncateUTF8(value, 512) }
	if value == "" { return "probe failed" }
	return value
}

func randomRemoteToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil { return "", err }
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func boundedRemoteLimit(limit int) int {
	if limit <= 0 { return 100 }
	if limit > 500 { return 500 }
	return limit
}
