package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestRemoteIngestKeyringDisabled(t *testing.T) {
	keyring, err := LoadRemoteIngestKeyring(RemoteIngestKeyringConfig{})
	require.NoError(t, err)
	require.False(t, keyring.Enabled())
	require.Empty(t, keyring.ActiveKeyID())

	_, err = keyring.EncryptString("secret")
	require.ErrorIs(t, err, ErrRemoteIngestKeyringDisabled)

	plain, err := keyring.DecryptString("legacy-secret")
	require.NoError(t, err)
	require.Equal(t, "legacy-secret", plain)

	_, err = keyring.DecryptString("ri:v1:key:not-base64")
	require.ErrorIs(t, err, ErrRemoteIngestKeyringDisabled)
}

func TestRemoteIngestKeyringEncryptDecryptAndAccountReadBoundary(t *testing.T) {
	keyring := loadTestRemoteIngestKeyring(t, "key-2026", map[string][]byte{
		"key-2026": remoteTestKey(1),
	})

	ciphertext, err := keyring.EncryptString("upstream-api-key")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(ciphertext, "ri:v1:key-2026:"))
	require.NotContains(t, ciphertext, "upstream-api-key")

	plaintext, err := keyring.DecryptString(ciphertext)
	require.NoError(t, err)
	require.Equal(t, "upstream-api-key", plaintext)

	account := &Account{Credentials: map[string]any{
		"api_key":  ciphertext,
		"base_url": "https://api.example.com/v1",
	}}
	require.Empty(t, account.GetCredential("api_key"), "encrypted values fail closed before decryptor registration")

	restore := RegisterRemoteIngestCredentialDecryptor(keyring)
	require.Equal(t, "upstream-api-key", account.GetCredential("api_key"))
	require.Equal(t, "https://api.example.com/v1", account.GetCredential("base_url"))
	require.Equal(t, ciphertext, account.Credentials["api_key"], "read-time decryption must not mutate persisted credentials")
	restore()

	require.Empty(t, account.GetCredential("api_key"))
}

func TestRemoteIngestKeyringSupportsRotationAndRejectsTampering(t *testing.T) {
	oldKeyring := loadTestRemoteIngestKeyring(t, "old", map[string][]byte{
		"old": remoteTestKey(2),
	})
	oldCiphertext, err := oldKeyring.EncryptString("old-secret")
	require.NoError(t, err)

	rotated := loadTestRemoteIngestKeyring(t, "new", map[string][]byte{
		"old": remoteTestKey(2),
		"new": remoteTestKey(3),
	})
	plaintext, err := rotated.DecryptString(oldCiphertext)
	require.NoError(t, err)
	require.Equal(t, "old-secret", plaintext)

	newCiphertext, err := rotated.EncryptString("new-secret")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(newCiphertext, "ri:v1:new:"))

	parts := strings.Split(newCiphertext, ":")
	require.Len(t, parts, 4)
	sealed, err := base64.StdEncoding.DecodeString(parts[3])
	require.NoError(t, err)
	sealed[len(sealed)-1] ^= 0xff
	parts[3] = base64.StdEncoding.EncodeToString(sealed)
	_, err = rotated.DecryptString(strings.Join(parts, ":"))
	require.Error(t, err)

	_, err = rotated.DecryptString(strings.Replace(newCiphertext, ":new:", ":missing:", 1))
	require.ErrorContains(t, err, "unknown key id")
}

func TestLoadRemoteIngestKeyringRejectsInvalidFiles(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "malformed", raw: `{`, want: "decode remote ingest keyring"},
		{name: "unknown field", raw: `{"active_key_id":"a","keys":{},"extra":true}`, want: "unknown field"},
		{name: "missing active", raw: `{"keys":{"a":"` + base64.StdEncoding.EncodeToString(remoteTestKey(1)) + `"}}`, want: "active_key_id is required"},
		{name: "invalid key id", raw: `{"active_key_id":"bad:id","keys":{"bad:id":"` + base64.StdEncoding.EncodeToString(remoteTestKey(1)) + `"}}`, want: "active_key_id is invalid"},
		{name: "missing active key", raw: `{"active_key_id":"a","keys":{"b":"` + base64.StdEncoding.EncodeToString(remoteTestKey(1)) + `"}}`, want: "active key is missing"},
		{name: "short key", raw: `{"active_key_id":"a","keys":{"a":"` + base64.StdEncoding.EncodeToString([]byte("short")) + `"}}`, want: "must be 32 bytes"},
		{name: "trailing JSON", raw: `{"active_key_id":"a","keys":{"a":"` + base64.StdEncoding.EncodeToString(remoteTestKey(1)) + `"}} {}`, want: "trailing JSON"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "keyring.json")
			require.NoError(t, os.WriteFile(path, []byte(tt.raw), 0o600))
			_, err := LoadRemoteIngestKeyring(RemoteIngestKeyringConfig{FilePath: path})
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestCloudflareAccessVerifierValidatesAndCachesJWKS(t *testing.T) {
	privateKey := generateRemoteAccessRSAKey(t)
	const kid = "access-key-1"
	var fetches atomic.Int32

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, cloudflareAccessJWKSPath, r.URL.Path)
		fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(cloudflareAccessJWKSet{
			Keys: []cloudflareAccessJWK{remoteAccessRSAJWK(kid, &privateKey.PublicKey)},
		}))
	}))
	defer server.Close()

	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	verifier, err := NewCloudflareAccessVerifier(CloudflareAccessVerifierConfig{
		TeamDomain: server.URL,
		Audience:   "access-audience",
		CacheTTL:   time.Hour,
		ClockSkew:  15 * time.Second,
	}, server.Client())
	require.NoError(t, err)
	verifier.now = func() time.Time { return now }

	assertion := signRemoteAccessToken(t, privateKey, kid, CloudflareAccessClaims{
		CommonName: "machine-token",
		TokenType:  "app",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    server.URL,
			Audience:  jwt.ClaimStrings{"access-audience"},
			IssuedAt:  jwt.NewNumericDate(now.Add(-time.Minute)),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
		},
	})

	identity, err := verifier.Verify(context.Background(), assertion)
	require.NoError(t, err)
	require.Equal(t, "machine-token", identity.Subject)
	require.Equal(t, "machine-token", identity.CommonName)
	require.True(t, identity.ExpiresAt.Equal(now.Add(5*time.Minute)))

	_, err = verifier.Verify(context.Background(), assertion)
	require.NoError(t, err)
	require.EqualValues(t, 1, fetches.Load(), "a valid cached kid should not refetch JWKS")
}

func TestCloudflareAccessVerifierUsesServiceTokenCommonName(t *testing.T) {
	privateKey := generateRemoteAccessRSAKey(t)
	server := newRemoteAccessJWKSServer(t, "key", &privateKey.PublicKey, nil)
	defer server.Close()
	now := time.Now().UTC().Truncate(time.Second)
	verifier := newRemoteAccessVerifier(t, server, now)

	assertion := signRemoteAccessToken(t, privateKey, "key", validRemoteAccessClaims(server.URL, now))
	identity, err := verifier.Verify(context.Background(), assertion)
	require.NoError(t, err)
	require.Equal(t, "service-common-name", identity.Subject)
}

func TestCloudflareAccessVerifierRejectsInvalidAssertions(t *testing.T) {
	privateKey := generateRemoteAccessRSAKey(t)
	server := newRemoteAccessJWKSServer(t, "key", &privateKey.PublicKey, nil)
	defer server.Close()
	now := time.Now().UTC().Truncate(time.Second)
	verifier := newRemoteAccessVerifier(t, server, now)

	tests := []struct {
		name   string
		mutate func(*CloudflareAccessClaims)
		want   string
	}{
		{name: "wrong issuer", mutate: func(c *CloudflareAccessClaims) { c.Issuer = "https://other.cloudflareaccess.com" }, want: "issuer"},
		{name: "wrong audience", mutate: func(c *CloudflareAccessClaims) { c.Audience = jwt.ClaimStrings{"other"} }, want: "audience"},
		{name: "expired", mutate: func(c *CloudflareAccessClaims) { c.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Minute)) }, want: "expired"},
		{name: "future nbf", mutate: func(c *CloudflareAccessClaims) { c.NotBefore = jwt.NewNumericDate(now.Add(time.Minute)) }, want: "not valid yet"},
		{name: "missing exp", mutate: func(c *CloudflareAccessClaims) { c.ExpiresAt = nil }, want: "exp"},
		{name: "missing iat", mutate: func(c *CloudflareAccessClaims) { c.IssuedAt = nil }, want: "iat"},
		{name: "wrong token type", mutate: func(c *CloudflareAccessClaims) { c.TokenType = "org" }, want: "application token"},
		{name: "identity token subject", mutate: func(c *CloudflareAccessClaims) { c.Subject = "user-id" }, want: "service token"},
		{name: "missing common name", mutate: func(c *CloudflareAccessClaims) { c.CommonName = "" }, want: "common_name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := validRemoteAccessClaims(server.URL, now)
			tt.mutate(&claims)
			assertion := signRemoteAccessToken(t, privateKey, "key", claims)
			_, err := verifier.Verify(context.Background(), assertion)
			require.ErrorContains(t, err, tt.want)
		})
	}

	otherKey := generateRemoteAccessRSAKey(t)
	badSignature := signRemoteAccessToken(t, otherKey, "key", validRemoteAccessClaims(server.URL, now))
	_, err := verifier.Verify(context.Background(), badSignature)
	require.Error(t, err)

	hsClaims := validRemoteAccessClaims(server.URL, now)
	hsToken := jwt.NewWithClaims(jwt.SigningMethodHS256, hsClaims)
	hsToken.Header["kid"] = "key"
	hsAssertion, err := hsToken.SignedString([]byte("not-an-rsa-key"))
	require.NoError(t, err)
	_, err = verifier.Verify(context.Background(), hsAssertion)
	require.ErrorContains(t, err, "signing method HS256 is invalid")
}

func TestCloudflareAccessVerifierRefreshesUnknownKidAndFailsClosed(t *testing.T) {
	privateKey := generateRemoteAccessRSAKey(t)
	var fetches atomic.Int32
	server := newRemoteAccessJWKSServer(t, "known", &privateKey.PublicKey, &fetches)
	defer server.Close()
	now := time.Now().UTC().Truncate(time.Second)
	verifier := newRemoteAccessVerifier(t, server, now)

	known := signRemoteAccessToken(t, privateKey, "known", validRemoteAccessClaims(server.URL, now))
	_, err := verifier.Verify(context.Background(), known)
	require.NoError(t, err)

	unknown := signRemoteAccessToken(t, privateKey, "rotated", validRemoteAccessClaims(server.URL, now))
	_, err = verifier.Verify(context.Background(), unknown)
	require.ErrorContains(t, err, "not found")
	require.EqualValues(t, 2, fetches.Load(), "an unknown cached kid should force one refresh")
}

func TestNewCloudflareAccessVerifierRejectsUnsafeConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  CloudflareAccessVerifierConfig
		want string
	}{
		{name: "missing domain", cfg: CloudflareAccessVerifierConfig{Audience: "aud"}, want: "team domain is required"},
		{name: "http domain", cfg: CloudflareAccessVerifierConfig{TeamDomain: "http://team.cloudflareaccess.com", Audience: "aud"}, want: "must use https"},
		{name: "domain path", cfg: CloudflareAccessVerifierConfig{TeamDomain: "https://team.cloudflareaccess.com/path", Audience: "aud"}, want: "only scheme and host"},
		{name: "missing audience", cfg: CloudflareAccessVerifierConfig{TeamDomain: "team.cloudflareaccess.com"}, want: "audience is required"},
		{name: "negative skew", cfg: CloudflareAccessVerifierConfig{TeamDomain: "team.cloudflareaccess.com", Audience: "aud", ClockSkew: -time.Second}, want: "cannot be negative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCloudflareAccessVerifier(tt.cfg, nil)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func loadTestRemoteIngestKeyring(t *testing.T, active string, keys map[string][]byte) *RemoteIngestKeyring {
	t.Helper()
	encoded := make(map[string]string, len(keys))
	for keyID, key := range keys {
		encoded[keyID] = base64.StdEncoding.EncodeToString(key)
	}
	raw, err := json.Marshal(remoteIngestKeyringFile{ActiveKeyID: active, Keys: encoded})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "remote-ingest-keyring.json")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	keyring, err := LoadRemoteIngestKeyring(RemoteIngestKeyringConfig{FilePath: path})
	require.NoError(t, err)
	return keyring
}

func remoteTestKey(fill byte) []byte {
	return []byte(strings.Repeat(string([]byte{fill}), 32))
}

func generateRemoteAccessRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

func remoteAccessRSAJWK(kid string, key *rsa.PublicKey) cloudflareAccessJWK {
	return cloudflareAccessJWK{
		Kty: "RSA",
		Kid: kid,
		Use: "sig",
		Alg: "RS256",
		N:   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}

func newRemoteAccessJWKSServer(t *testing.T, kid string, key *rsa.PublicKey, fetches *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fetches != nil {
			fetches.Add(1)
		}
		require.Equal(t, cloudflareAccessJWKSPath, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(cloudflareAccessJWKSet{
			Keys: []cloudflareAccessJWK{remoteAccessRSAJWK(kid, key)},
		}))
	}))
}

func newRemoteAccessVerifier(t *testing.T, server *httptest.Server, now time.Time) *CloudflareAccessVerifier {
	t.Helper()
	verifier, err := NewCloudflareAccessVerifier(CloudflareAccessVerifierConfig{
		TeamDomain: server.URL,
		Audience:   "access-audience",
		CacheTTL:   time.Hour,
	}, server.Client())
	require.NoError(t, err)
	verifier.now = func() time.Time { return now }
	return verifier
}

func validRemoteAccessClaims(issuer string, now time.Time) CloudflareAccessClaims {
	return CloudflareAccessClaims{
		CommonName: "service-common-name",
		TokenType:  "app",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Audience:  jwt.ClaimStrings{"access-audience"},
			IssuedAt:  jwt.NewNumericDate(now.Add(-time.Minute)),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
		},
	}
}

func signRemoteAccessToken(t *testing.T, key *rsa.PrivateKey, kid string, claims CloudflareAccessClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	require.NoError(t, err)
	return signed
}
