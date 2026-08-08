package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	remoteIngestCiphertextPrefix = "ri:v1:"
	remoteIngestCredentialAAD    = "sub2api:remote-ingest:credential:v1"
	cloudflareAccessJWKSPath     = "/cdn-cgi/access/certs"
	maxCloudflareAccessJWKSBytes = 1 << 20
)

var (
	ErrRemoteIngestKeyringDisabled  = errors.New("remote ingest keyring is disabled")
	ErrRemoteIngestCiphertextFormat = errors.New("invalid remote ingest ciphertext")
)

// RemoteIngestKeyringConfig identifies the read-only Docker Secret containing
// the remote-ingest encryption keys. An empty path intentionally disables the
// keyring so deployments can keep the feature switched off.
type RemoteIngestKeyringConfig struct {
	FilePath string
}

type remoteIngestKeyringFile struct {
	ActiveKeyID string            `json:"active_key_id"`
	Keys        map[string]string `json:"keys"`
}

// RemoteIngestKeyring encrypts newly ingested credentials with the active key
// and retains old keys for decrypting data written before a rotation.
type RemoteIngestKeyring struct {
	activeKeyID string
	keys        map[string][]byte
}

// LoadRemoteIngestKeyring loads and validates a versioned AES-256-GCM keyring.
func LoadRemoteIngestKeyring(cfg RemoteIngestKeyringConfig) (*RemoteIngestKeyring, error) {
	path := strings.TrimSpace(cfg.FilePath)
	if path == "" {
		return &RemoteIngestKeyring{}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open remote ingest keyring: %w", err)
	}
	defer func() { _ = f.Close() }()

	var raw remoteIngestKeyringFile
	decoder := json.NewDecoder(io.LimitReader(f, maxCloudflareAccessJWKSBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode remote ingest keyring: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode remote ingest keyring: %w", err)
	}

	activeKeyID := strings.TrimSpace(raw.ActiveKeyID)
	if activeKeyID == "" {
		return nil, errors.New("remote ingest keyring active_key_id is required")
	}
	if activeKeyID != raw.ActiveKeyID || !validRemoteIngestKeyID(activeKeyID) {
		return nil, errors.New("remote ingest keyring active_key_id is invalid")
	}
	if len(raw.Keys) == 0 {
		return nil, errors.New("remote ingest keyring keys are required")
	}

	keys := make(map[string][]byte, len(raw.Keys))
	for keyID, encoded := range raw.Keys {
		if keyID != strings.TrimSpace(keyID) || !validRemoteIngestKeyID(keyID) {
			return nil, fmt.Errorf("remote ingest keyring key id %q is invalid", keyID)
		}
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			return nil, fmt.Errorf("decode remote ingest key %q: %w", keyID, err)
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("remote ingest key %q must be 32 bytes", keyID)
		}
		keys[keyID] = append([]byte(nil), key...)
	}
	if _, ok := keys[activeKeyID]; !ok {
		return nil, errors.New("remote ingest keyring active key is missing")
	}

	return &RemoteIngestKeyring{activeKeyID: activeKeyID, keys: keys}, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("unexpected trailing JSON value")
}

func validRemoteIngestKeyID(keyID string) bool {
	if len(keyID) == 0 || len(keyID) > 64 {
		return false
	}
	for _, r := range keyID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func (k *RemoteIngestKeyring) Enabled() bool {
	return k != nil && k.activeKeyID != "" && len(k.keys) > 0
}

func (k *RemoteIngestKeyring) ActiveKeyID() string {
	if k == nil {
		return ""
	}
	return k.activeKeyID
}

// EncryptString returns ri:v1:<key-id>:<base64 nonce+ciphertext+tag>.
func (k *RemoteIngestKeyring) EncryptString(plaintext string) (string, error) {
	if !k.Enabled() {
		return "", ErrRemoteIngestKeyringDisabled
	}
	aead, err := remoteIngestAEAD(k.keys[k.activeKeyID])
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate remote ingest nonce: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, []byte(plaintext), []byte(remoteIngestCredentialAAD))
	return remoteIngestCiphertextPrefix + k.activeKeyID + ":" + base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptString passes through legacy plaintext values and decrypts only values
// bearing the remote-ingest ciphertext prefix.
func (k *RemoteIngestKeyring) DecryptString(value string) (string, error) {
	if !IsRemoteIngestCiphertext(value) {
		return value, nil
	}
	if !k.Enabled() {
		return "", ErrRemoteIngestKeyringDisabled
	}

	remainder := strings.TrimPrefix(value, remoteIngestCiphertextPrefix)
	keyID, encoded, ok := strings.Cut(remainder, ":")
	if !ok || !validRemoteIngestKeyID(keyID) || encoded == "" {
		return "", ErrRemoteIngestCiphertextFormat
	}
	key, ok := k.keys[keyID]
	if !ok {
		return "", fmt.Errorf("remote ingest ciphertext references unknown key id %q", keyID)
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("%w: invalid base64", ErrRemoteIngestCiphertextFormat)
	}
	aead, err := remoteIngestAEAD(key)
	if err != nil {
		return "", err
	}
	if len(sealed) < aead.NonceSize()+aead.Overhead() {
		return "", ErrRemoteIngestCiphertextFormat
	}
	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(remoteIngestCredentialAAD))
	if err != nil {
		return "", errors.New("authenticate remote ingest ciphertext")
	}
	return string(plaintext), nil
}

func remoteIngestAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize remote ingest cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize remote ingest GCM: %w", err)
	}
	return aead, nil
}

func IsRemoteIngestCiphertext(value string) bool {
	return strings.HasPrefix(value, remoteIngestCiphertextPrefix)
}

// RemoteIngestCredentialDecryptor is deliberately narrow so Account can
// decrypt at its existing read boundary without changing stored credentials.
type RemoteIngestCredentialDecryptor interface {
	DecryptString(value string) (string, error)
}

type remoteIngestDecryptorRegistration struct {
	decryptor RemoteIngestCredentialDecryptor
}

var remoteIngestDecryptorRegistry struct {
	sync.RWMutex
	current *remoteIngestDecryptorRegistration
}

// RegisterRemoteIngestCredentialDecryptor installs the process-wide credential
// decryptor. The returned function restores the previous registration, which is
// useful for isolated tests and controlled application shutdown.
func RegisterRemoteIngestCredentialDecryptor(decryptor RemoteIngestCredentialDecryptor) func() {
	registration := &remoteIngestDecryptorRegistration{decryptor: decryptor}
	remoteIngestDecryptorRegistry.Lock()
	previous := remoteIngestDecryptorRegistry.current
	remoteIngestDecryptorRegistry.current = registration
	remoteIngestDecryptorRegistry.Unlock()

	return func() {
		remoteIngestDecryptorRegistry.Lock()
		if remoteIngestDecryptorRegistry.current == registration {
			remoteIngestDecryptorRegistry.current = previous
		}
		remoteIngestDecryptorRegistry.Unlock()
	}
}

func decryptRemoteIngestCredential(value string) (string, error) {
	if !IsRemoteIngestCiphertext(value) {
		return value, nil
	}
	remoteIngestDecryptorRegistry.RLock()
	registration := remoteIngestDecryptorRegistry.current
	remoteIngestDecryptorRegistry.RUnlock()
	if registration == nil || registration.decryptor == nil {
		return "", ErrRemoteIngestKeyringDisabled
	}
	return registration.decryptor.DecryptString(value)
}

// CloudflareAccessVerifierConfig configures strict origin verification for a
// single Cloudflare Access application audience.
type CloudflareAccessVerifierConfig struct {
	TeamDomain string
	Audience   string
	CacheTTL   time.Duration
	ClockSkew  time.Duration
}

type CloudflareAccessClaims struct {
	CommonName string `json:"common_name,omitempty"`
	TokenType  string `json:"type,omitempty"`
	jwt.RegisteredClaims
}

type CloudflareAccessIdentity struct {
	Subject    string
	CommonName string
	ExpiresAt  time.Time
}

type cloudflareAccessJWKSet struct {
	Keys []cloudflareAccessJWK `json:"keys"`
}

type cloudflareAccessJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// CloudflareAccessVerifier validates Cf-Access-Jwt-Assertion tokens and caches
// the team's public signing keys. Cache expiry and an unknown kid both trigger
// a fresh JWKS fetch; fetch failures never fall back to stale or unverified data.
type CloudflareAccessVerifier struct {
	issuer    string
	audience  string
	jwksURL   string
	cacheTTL  time.Duration
	clockSkew time.Duration
	client    *http.Client
	now       func() time.Time

	mu         sync.Mutex
	keys       map[string]*rsa.PublicKey
	cacheUntil time.Time
}

func NewCloudflareAccessVerifier(cfg CloudflareAccessVerifierConfig, client *http.Client) (*CloudflareAccessVerifier, error) {
	issuer, err := normalizeCloudflareTeamDomain(cfg.TeamDomain)
	if err != nil {
		return nil, err
	}
	audience := strings.TrimSpace(cfg.Audience)
	if audience == "" {
		return nil, errors.New("cloudflare access audience is required")
	}
	if cfg.ClockSkew < 0 {
		return nil, errors.New("cloudflare access clock skew cannot be negative")
	}
	cacheTTL := cfg.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Minute
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	} else {
		copied := *client
		client = &copied
		if client.Timeout <= 0 {
			client.Timeout = 10 * time.Second
		}
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("cloudflare access JWKS redirect refused")
	}

	return &CloudflareAccessVerifier{
		issuer:    issuer,
		audience:  audience,
		jwksURL:   issuer + cloudflareAccessJWKSPath,
		cacheTTL:  cacheTTL,
		clockSkew: cfg.ClockSkew,
		client:    client,
		now:       time.Now,
	}, nil
}

func normalizeCloudflareTeamDomain(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("cloudflare access team domain is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse cloudflare access team domain: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", errors.New("cloudflare access team domain must use https")
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("cloudflare access team domain must contain only scheme and host")
	}
	return "https://" + u.Host, nil
}

func (v *CloudflareAccessVerifier) Verify(ctx context.Context, assertion string) (*CloudflareAccessIdentity, error) {
	if v == nil {
		return nil, errors.New("cloudflare access verifier is not configured")
	}
	assertion = strings.TrimSpace(assertion)
	if assertion == "" {
		return nil, errors.New("cloudflare access assertion is required")
	}

	claims := &CloudflareAccessClaims{}
	parsed, err := jwt.ParseWithClaims(
		assertion,
		claims,
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodRS256 || token.Method.Alg() != jwt.SigningMethodRS256.Alg() {
				return nil, errors.New("cloudflare access token must use RS256")
			}
			kid, _ := token.Header["kid"].(string)
			kid = strings.TrimSpace(kid)
			if kid == "" {
				return nil, errors.New("cloudflare access token kid is required")
			}
			return v.publicKey(ctx, kid)
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(v.clockSkew),
		jwt.WithTimeFunc(v.now),
	)
	if err != nil {
		return nil, fmt.Errorf("verify cloudflare access assertion: %w", err)
	}
	if !parsed.Valid {
		return nil, errors.New("cloudflare access assertion is invalid")
	}
	if claims.ExpiresAt == nil {
		return nil, errors.New("cloudflare access assertion missing exp")
	}
	if claims.IssuedAt == nil {
		return nil, errors.New("cloudflare access assertion missing iat")
	}
	if strings.TrimSpace(claims.TokenType) != "app" {
		return nil, errors.New("cloudflare access assertion is not an application token")
	}
	if strings.TrimSpace(claims.Subject) != "" {
		return nil, errors.New("cloudflare access assertion is not a service token")
	}
	commonName := strings.TrimSpace(claims.CommonName)
	if commonName == "" {
		return nil, errors.New("cloudflare access assertion missing service token common_name")
	}
	return &CloudflareAccessIdentity{
		Subject:    commonName,
		CommonName: commonName,
		ExpiresAt:  claims.ExpiresAt.Time,
	}, nil
}

func (v *CloudflareAccessVerifier) publicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	now := v.now()
	v.mu.Lock()
	defer v.mu.Unlock()

	if now.Before(v.cacheUntil) {
		if key := v.keys[kid]; key != nil {
			return key, nil
		}
	}
	keys, err := v.fetchKeys(ctx)
	if err != nil {
		return nil, err
	}
	v.keys = keys
	v.cacheUntil = now.Add(v.cacheTTL)
	key := keys[kid]
	if key == nil {
		return nil, fmt.Errorf("cloudflare access signing key %q not found", kid)
	}
	return key, nil
}

func (v *CloudflareAccessVerifier) fetchKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create cloudflare access JWKS request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch cloudflare access JWKS: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch cloudflare access JWKS: status %d", resp.StatusCode)
	}

	var set cloudflareAccessJWKSet
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxCloudflareAccessJWKSBytes))
	if err := decoder.Decode(&set); err != nil {
		return nil, fmt.Errorf("decode cloudflare access JWKS: %w", err)
	}
	if len(set.Keys) == 0 {
		return nil, errors.New("cloudflare access JWKS has no keys")
	}

	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, jwk := range set.Keys {
		kid := strings.TrimSpace(jwk.Kid)
		if kid == "" || !strings.EqualFold(strings.TrimSpace(jwk.Kty), "RSA") {
			continue
		}
		if use := strings.TrimSpace(jwk.Use); use != "" && !strings.EqualFold(use, "sig") {
			continue
		}
		if alg := strings.TrimSpace(jwk.Alg); alg != "" && !strings.EqualFold(alg, jwt.SigningMethodRS256.Alg()) {
			continue
		}
		key, err := jwk.rsaPublicKey()
		if err != nil {
			return nil, fmt.Errorf("decode cloudflare access JWK %q: %w", kid, err)
		}
		keys[kid] = key
	}
	if len(keys) == 0 {
		return nil, errors.New("cloudflare access JWKS has no usable RS256 keys")
	}
	return keys, nil
}

func (j cloudflareAccessJWK) rsaPublicKey() (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(j.N))
	if err != nil || len(nBytes) == 0 {
		return nil, errors.New("invalid RSA modulus")
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(j.E))
	if err != nil || len(eBytes) == 0 || len(eBytes) > 4 {
		return nil, errors.New("invalid RSA exponent")
	}
	exponent := 0
	for _, b := range eBytes {
		exponent = exponent<<8 | int(b)
	}
	if exponent < 3 || exponent%2 == 0 {
		return nil, errors.New("invalid RSA exponent")
	}
	modulus := new(big.Int).SetBytes(nBytes)
	if modulus.Sign() <= 0 {
		return nil, errors.New("invalid RSA modulus")
	}
	return &rsa.PublicKey{N: modulus, E: exponent}, nil
}
