package main

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

const (
	listenAddress                  = ":8090"
	maxHeartbeatBodyBytes          = 1 << 20
	maxHeartbeatKeys               = 100
	maxClockSkew                   = 15 * time.Minute
	minHeartbeatInterval           = 10 * time.Second
	proxyPageSize                  = 1000
	defaultWorkerCount             = 2
	defaultProxyProbeWorkers       = 10
	defaultProxyProbeTimeout       = 5 * time.Second
	defaultProvisionTimeout        = 20 * time.Minute
	defaultPollInterval            = time.Second
	defaultMaxAttempts             = 5
	defaultProxySweepTTL           = 5 * time.Minute
	defaultDeepSeekGroupID   int64 = 12
	defaultProxyGroupID      int64 = 1
)

type config struct {
	listenAddr         string
	databaseURL        string
	sub2apiBaseURL     string
	sub2apiAuthHeader  string
	sub2apiToken       string
	allowedSessionKey  string
	vaultURL           *url.URL
	allowInsecureVault bool
	deepSeekGroupID    int64
	proxyGroupID       int64
	workerCount        int
	proxyProbeWorkers  int
	proxyProbeTimeout  time.Duration
	provisionTimeout   time.Duration
	pollInterval       time.Duration
	maxAttempts        int
	proxySweepTTL      time.Duration
}

func loadConfig() (config, error) {
	cfg := config{
		listenAddr:         envOr("HEARTBEAT_LISTEN_ADDR", listenAddress),
		sub2apiBaseURL:     strings.TrimRight(envOr("SUB2API_BASE_URL", "http://sub2api:8080"), "/"),
		sub2apiAuthHeader:  envOr("SUB2API_ADMIN_AUTH_HEADER", "x-api-key"),
		allowedSessionKey:  strings.TrimSpace(os.Getenv("HEARTBEAT_ALLOWED_SESSION_KEY")),
		allowInsecureVault: envBool("HEARTBEAT_ALLOW_INSECURE_VAULT", false),
		deepSeekGroupID:    envInt64("HEARTBEAT_DEEPSEEK_GROUP_ID", defaultDeepSeekGroupID),
		proxyGroupID:       envInt64("HEARTBEAT_PROXY_GROUP_ID", defaultProxyGroupID),
		workerCount:        envInt("HEARTBEAT_WORKER_COUNT", defaultWorkerCount),
		proxyProbeWorkers:  envInt("HEARTBEAT_PROXY_PROBE_WORKERS", defaultProxyProbeWorkers),
		proxyProbeTimeout:  envDuration("HEARTBEAT_PROXY_PROBE_TIMEOUT", defaultProxyProbeTimeout),
		provisionTimeout:   envDuration("HEARTBEAT_PROVISION_TIMEOUT", defaultProvisionTimeout),
		pollInterval:       envDuration("HEARTBEAT_POLL_INTERVAL", defaultPollInterval),
		maxAttempts:        envInt("HEARTBEAT_MAX_ATTEMPTS", defaultMaxAttempts),
		proxySweepTTL:      envDuration("HEARTBEAT_PROXY_SWEEP_TTL", defaultProxySweepTTL),
	}

	if cfg.allowedSessionKey == "" {
		return config{}, errors.New("HEARTBEAT_ALLOWED_SESSION_KEY is required")
	}
	if len(cfg.allowedSessionKey) != 32 {
		return config{}, errors.New("HEARTBEAT_ALLOWED_SESSION_KEY must be the source session key")
	}
	if _, err := hex.DecodeString(cfg.allowedSessionKey); err != nil {
		return config{}, errors.New("HEARTBEAT_ALLOWED_SESSION_KEY must be hexadecimal")
	}
	if cfg.sub2apiToken = strings.TrimSpace(os.Getenv("SUB2API_ADMIN_TOKEN")); cfg.sub2apiToken == "" {
		return config{}, errors.New("SUB2API_ADMIN_TOKEN is required")
	}
	if cfg.databaseURL = strings.TrimSpace(os.Getenv("HEARTBEAT_DATABASE_URL")); cfg.databaseURL == "" {
		var err error
		cfg.databaseURL, err = postgresURLFromEnv()
		if err != nil {
			return config{}, err
		}
	}
	vaultURL, err := url.Parse(strings.TrimSpace(os.Getenv("HEARTBEAT_VAULT_URL")))
	if err != nil || vaultURL == nil || vaultURL.Host == "" {
		return config{}, errors.New("HEARTBEAT_VAULT_URL must be an absolute URL")
	}
	if vaultURL.Scheme != "https" && !(vaultURL.Scheme == "http" && cfg.allowInsecureVault) {
		return config{}, errors.New("HEARTBEAT_VAULT_URL must use HTTPS unless HEARTBEAT_ALLOW_INSECURE_VAULT=true")
	}
	cfg.vaultURL = vaultURL
	if cfg.deepSeekGroupID <= 0 || cfg.proxyGroupID <= 0 || cfg.workerCount < 1 || cfg.proxyProbeWorkers < 1 || cfg.maxAttempts < 1 || cfg.proxySweepTTL <= 0 {
		return config{}, errors.New("heartbeat numeric configuration must be positive")
	}
	return cfg, nil
}

func postgresURLFromEnv() (string, error) {
	host := strings.TrimSpace(os.Getenv("POSTGRES_HOST"))
	user := strings.TrimSpace(os.Getenv("POSTGRES_USER"))
	password := os.Getenv("POSTGRES_PASSWORD")
	database := strings.TrimSpace(os.Getenv("POSTGRES_DB"))
	if host == "" || user == "" || password == "" || database == "" {
		return "", errors.New("HEARTBEAT_DATABASE_URL or POSTGRES_HOST/USER/PASSWORD/DB is required")
	}
	port := envOr("POSTGRES_PORT", "5432")
	u := &url.URL{Scheme: "postgres", User: url.UserPassword(user, password), Host: host + ":" + port, Path: database}
	q := u.Query()
	q.Set("sslmode", envOr("POSTGRES_SSLMODE", "disable"))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func envInt64(name string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

type heartbeatRequest struct {
	SessionKey string         `json:"session_key"`
	Timestamp  int64          `json:"ts"`
	Keys       []heartbeatKey `json:"keys"`
}

type heartbeatKey struct {
	Fingerprint string  `json:"fp"`
	Provider    string  `json:"provider"`
	Balance     float64 `json:"balance"`
	CheckedAt   string  `json:"checked_at"`
}

type job struct {
	ID          int64
	Fingerprint string
	Attempts    int
	AccountID   sql.NullInt64
	ProxyID     sql.NullInt64
}

type store struct{ db *sql.DB }

func (s *store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS heartbeat_provision_jobs (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    fingerprint CHAR(24) NOT NULL,
    session_key_hash CHAR(64) NOT NULL,
    source_balance DOUBLE PRECISION NULL,
    source_checked_at TIMESTAMPTZ NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lock_owner TEXT NULL,
    locked_until TIMESTAMPTZ NULL,
    account_id BIGINT NULL,
    proxy_id BIGINT NULL,
    last_error TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ NULL,
    CONSTRAINT heartbeat_provision_jobs_provider_fingerprint_unique UNIQUE (provider, fingerprint),
    CONSTRAINT heartbeat_provision_jobs_status_check CHECK (status IN ('queued', 'processing', 'retry', 'complete', 'failed'))
);
CREATE INDEX IF NOT EXISTS heartbeat_provision_jobs_ready_idx
    ON heartbeat_provision_jobs (available_at, id)
    WHERE status IN ('queued', 'retry');
CREATE INDEX IF NOT EXISTS heartbeat_provision_jobs_lease_idx
    ON heartbeat_provision_jobs (locked_until)
    WHERE status = 'processing';`)
	return err
}

func (s *store) enqueue(ctx context.Context, key heartbeatKey, sessionHash string) error {
	checkedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(key.CheckedAt))
	var checkedAtArg any
	if err != nil {
		checkedAtArg = nil
	} else {
		checkedAtArg = checkedAt
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO heartbeat_provision_jobs (provider, fingerprint, session_key_hash, source_balance, source_checked_at)
VALUES ('ds', $1, $2, $3, NULLIF($4::timestamptz, 'epoch'::timestamptz))
ON CONFLICT (provider, fingerprint) DO UPDATE SET
    source_balance = EXCLUDED.source_balance,
    source_checked_at = COALESCE(EXCLUDED.source_checked_at, heartbeat_provision_jobs.source_checked_at),
	updated_at = NOW()`,
		strings.ToLower(key.Fingerprint), sessionHash, key.Balance, checkedAtArg)
	return err
}

func (s *store) claim(ctx context.Context, owner string, lease time.Duration) (*job, error) {
	row := s.db.QueryRowContext(ctx, `
WITH candidate AS (
    SELECT id
    FROM heartbeat_provision_jobs
    WHERE (status IN ('queued', 'retry') AND available_at <= NOW())
       OR (status = 'processing' AND locked_until < NOW())
    ORDER BY available_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE heartbeat_provision_jobs AS job
SET status = 'processing', attempts = attempts + 1, lock_owner = $1,
    locked_until = NOW() + $2::interval, updated_at = NOW()
FROM candidate
WHERE job.id = candidate.id
RETURNING job.id, job.fingerprint, job.attempts, job.account_id, job.proxy_id`, owner, intervalLiteral(lease))
	claimed := &job{}
	if err := row.Scan(&claimed.ID, &claimed.Fingerprint, &claimed.Attempts, &claimed.AccountID, &claimed.ProxyID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return claimed, nil
}

func (s *store) setProxy(ctx context.Context, id, proxyID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE heartbeat_provision_jobs SET proxy_id = $2, updated_at = NOW() WHERE id = $1`, id, proxyID)
	return err
}

func (s *store) setAccount(ctx context.Context, id, accountID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE heartbeat_provision_jobs SET account_id = $2, updated_at = NOW() WHERE id = $1`, id, accountID)
	return err
}

func (s *store) complete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE heartbeat_provision_jobs SET status = 'complete', lock_owner = NULL, locked_until = NULL, last_error = NULL, completed_at = NOW(), updated_at = NOW() WHERE id = $1`, id)
	return err
}

func (s *store) retry(ctx context.Context, item *job, cause error, maxAttempts int) error {
	status := "retry"
	availableAt := time.Now().Add(retryDelay(item.Attempts))
	if item.Attempts >= maxAttempts {
		status = "failed"
		availableAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE heartbeat_provision_jobs
SET status = $2, available_at = $3, lock_owner = NULL, locked_until = NULL, last_error = $4, updated_at = NOW()
WHERE id = $1`, item.ID, status, availableAt, safeError(cause))
	return err
}

func intervalLiteral(d time.Duration) string {
	return fmt.Sprintf("%f seconds", d.Seconds())
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		return 30 * time.Second
	}
	seconds := math.Min(30*math.Pow(2, float64(attempt-1)), 1800)
	return time.Duration(seconds) * time.Second
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 400 {
		message = message[:400]
	}
	return strings.NewReplacer("sk-", "[redacted]-", "session_key", "session").Replace(message)
}

type sub2apiClient struct {
	baseURL string
	header  string
	token   string
	http    *http.Client
}

func newSub2APIClient(cfg config) *sub2apiClient {
	return &sub2apiClient{
		baseURL: cfg.sub2apiBaseURL,
		header:  cfg.sub2apiAuthHeader,
		token:   cfg.sub2apiToken,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *sub2apiClient) request(ctx context.Context, method, path string, body any, idempotencyKey string, target any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(payload))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set(c.header, c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("sub2api %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxHeartbeatBodyBytes+1))
	if readErr != nil {
		return readErr
	}
	if len(data) > maxHeartbeatBodyBytes {
		return fmt.Errorf("sub2api %s %s response too large", method, path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sub2api %s %s returned HTTP %d", method, path, resp.StatusCode)
	}
	if target == nil {
		return nil
	}
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("decode sub2api response: %w", err)
	}
	if envelope.Code != 0 {
		return fmt.Errorf("sub2api %s %s returned code %d", method, path, envelope.Code)
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return fmt.Errorf("decode sub2api data: %w", err)
	}
	return nil
}

type proxyPage struct {
	Items []proxy `json:"items"`
	Total int64   `json:"total"`
}

type proxy struct {
	ID int64 `json:"id"`
}

type proxyTestResult struct {
	Success   bool  `json:"success"`
	LatencyMs int64 `json:"latency_ms"`
}

type accountResponse struct {
	ID int64 `json:"id"`
}

type accountGroup struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Status   string `json:"status"`
}

type deepSeekBalanceResponse struct {
	IsAvailable bool `json:"is_available"`
}

// ensureNoDefaultDeepSeekGroup prevents Sub2API's create endpoint from placing
// an account in an implicit default group before the balance validation succeeds.
func (c *sub2apiClient) ensureNoDefaultDeepSeekGroup(ctx context.Context) error {
	var groups []accountGroup
	if err := c.request(ctx, http.MethodGet, "/api/v1/admin/groups/all", nil, "", &groups); err != nil {
		return err
	}
	for _, group := range groups {
		if strings.EqualFold(strings.TrimSpace(group.Name), "deepseek-default") && strings.EqualFold(strings.TrimSpace(group.Platform), "deepseek") {
			return errors.New("deepseek-default group exists; refusing unverified account creation")
		}
	}
	return nil
}

func (c *sub2apiClient) validateDeepSeekGroup(ctx context.Context, groupID int64) error {
	var groups []accountGroup
	if err := c.request(ctx, http.MethodGet, "/api/v1/admin/groups/all", nil, "", &groups); err != nil {
		return err
	}
	for _, group := range groups {
		if group.ID == groupID && strings.EqualFold(strings.TrimSpace(group.Platform), "deepseek") {
			return nil
		}
	}
	return fmt.Errorf("configured DeepSeek group %d is unavailable", groupID)
}

func (c *sub2apiClient) activeProxies(ctx context.Context, groupID int64) ([]proxy, error) {
	result := make([]proxy, 0)
	for page := 1; ; page++ {
		var payload proxyPage
		path := fmt.Sprintf("/api/v1/admin/proxies?status=active&group_id=%d&page=%d&page_size=%d&sort_by=id&sort_order=asc", groupID, page, proxyPageSize)
		if err := c.request(ctx, http.MethodGet, path, nil, "", &payload); err != nil {
			return nil, err
		}
		result = append(result, payload.Items...)
		if int64(len(result)) >= payload.Total || len(payload.Items) == 0 {
			return result, nil
		}
	}
}

func (c *sub2apiClient) testProxy(ctx context.Context, id int64) (proxyTestResult, error) {
	var result proxyTestResult
	err := c.request(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/proxies/%d/test", id), nil, "", &result)
	return result, err
}

func (c *sub2apiClient) createUnassignedDeepSeekAccount(ctx context.Context, fingerprint, key string, proxyID int64) (int64, error) {
	if err := c.ensureNoDefaultDeepSeekGroup(ctx); err != nil {
		return 0, err
	}
	request := map[string]any{
		"name":        "heartbeat-ds-" + fingerprint,
		"platform":    "deepseek",
		"type":        "apikey",
		"credentials": map[string]any{"api_key": key, "pool_mode": true, "pool_mode_retry_count": 3, "pool_mode_retry_status_codes": []int{401, 403, 429}},
		"extra":       map[string]any{"heartbeat_fp": fingerprint, "heartbeat_source": "key-checker"},
		"proxy_id":    proxyID,
		"concurrency": 3,
		"priority":    50,
	}
	var account accountResponse
	if err := c.request(ctx, http.MethodPost, "/api/v1/admin/accounts", request, "heartbeat-provision-"+fingerprint, &account); err != nil {
		return 0, err
	}
	if account.ID <= 0 {
		return 0, errors.New("sub2api create account returned no ID")
	}
	return account.ID, nil
}

func (c *sub2apiClient) updateAccount(ctx context.Context, accountID int64, body map[string]any) error {
	return c.request(ctx, http.MethodPut, fmt.Sprintf("/api/v1/admin/accounts/%d", accountID), body, "", &struct{}{})
}

func (c *sub2apiClient) verifyDeepSeekBalance(ctx context.Context, accountID int64) error {
	var result deepSeekBalanceResponse
	if err := c.request(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/deepseek/accounts/%d/balance", accountID), nil, "", &result); err != nil {
		return err
	}
	if !result.IsAvailable {
		return errors.New("DeepSeek balance reports unavailable")
	}
	return nil
}

type vaultResponse struct {
	OK   bool       `json:"ok"`
	Keys []vaultKey `json:"keys"`
}

type vaultKey struct {
	Key      string `json:"key"`
	Provider string `json:"provider"`
}

func fetchVaultKey(ctx context.Context, cfg config, fingerprint string) (string, error) {
	u := *cfg.vaultURL
	query := u.Query()
	query.Set("session_key", cfg.allowedSessionKey)
	u.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Do(request)
	if err != nil {
		// url.Error includes the query string, which contains the session key.
		return "", errors.New("vault request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vault returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxHeartbeatBodyBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxHeartbeatBodyBytes {
		return "", errors.New("vault response too large")
	}
	var payload vaultResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode vault response: %w", err)
	}
	if !payload.OK {
		return "", errors.New("vault returned unsuccessful response")
	}
	for _, candidate := range payload.Keys {
		if strings.EqualFold(strings.TrimSpace(candidate.Provider), "ds") && fingerprintForKey(candidate.Key) == fingerprint {
			return candidate.Key, nil
		}
	}
	return "", errors.New("matching DeepSeek key is not present in vault")
}

func fingerprintForKey(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])[:24]
}

func sessionHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func validFingerprint(value string) bool {
	if len(value) != 24 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func probeLowLatencyProxies(ctx context.Context, client *sub2apiClient, candidates []proxy, workers int, timeout time.Duration) ([]int64, error) {
	if len(candidates) == 0 {
		return nil, errors.New("no active proxies in target group")
	}
	type probeResult struct {
		id      int64
		latency int64
	}
	input := make(chan proxy)
	output := make(chan probeResult, len(candidates))
	var waitGroup sync.WaitGroup
	for i := 0; i < workers; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for item := range input {
				probeCtx, cancel := context.WithTimeout(ctx, timeout)
				result, err := client.testProxy(probeCtx, item.ID)
				cancel()
				if err == nil && result.Success && result.LatencyMs > 0 {
					output <- probeResult{id: item.ID, latency: result.LatencyMs}
				}
			}
		}()
	}
	go func() {
		defer close(input)
		for _, item := range candidates {
			select {
			case input <- item:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		waitGroup.Wait()
		close(output)
	}()

	results := make([]probeResult, 0, len(candidates))
	for result := range output {
		results = append(results, result)
	}
	if len(results) == 0 {
		return nil, errors.New("no proxy completed a successful latency probe")
	}
	sort.Slice(results, func(i, j int) bool { return results[i].latency < results[j].latency })
	tierSize := int(math.Ceil(float64(len(results)) * 0.10))
	if tierSize < 1 {
		tierSize = 1
	}
	tier := make([]int64, tierSize)
	for i := range tier {
		tier[i] = results[i].id
	}
	return tier, nil
}

func chooseRandomProxy(candidates []int64) (int64, error) {
	if len(candidates) == 0 {
		return 0, errors.New("no low-latency proxy candidates")
	}
	choice, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(len(candidates))))
	if err != nil {
		return 0, fmt.Errorf("choose proxy: %w", err)
	}
	return candidates[choice.Int64()], nil
}

func selectLowLatencyProxy(ctx context.Context, client *sub2apiClient, candidates []proxy, workers int, timeout time.Duration) (int64, error) {
	tier, err := probeLowLatencyProxies(ctx, client, candidates, workers, timeout)
	if err != nil {
		return 0, err
	}
	return chooseRandomProxy(tier)
}

type proxySelector struct {
	client  *sub2apiClient
	groupID int64
	workers int
	timeout time.Duration
	ttl     time.Duration

	mu         sync.RWMutex
	refreshMu  sync.Mutex
	candidates []int64
	expiresAt  time.Time
}

func (s *proxySelector) choose(ctx context.Context) (int64, error) {
	now := time.Now()
	s.mu.RLock()
	cached := append([]int64(nil), s.candidates...)
	valid := len(cached) > 0 && now.Before(s.expiresAt)
	s.mu.RUnlock()
	if valid {
		return chooseRandomProxy(cached)
	}

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	now = time.Now()
	s.mu.RLock()
	cached = append([]int64(nil), s.candidates...)
	valid = len(cached) > 0 && now.Before(s.expiresAt)
	s.mu.RUnlock()
	if valid {
		return chooseRandomProxy(cached)
	}

	proxies, err := s.client.activeProxies(ctx, s.groupID)
	if err != nil {
		return 0, err
	}
	tier, err := probeLowLatencyProxies(ctx, s.client, proxies, s.workers, s.timeout)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.candidates = append([]int64(nil), tier...)
	s.expiresAt = time.Now().Add(s.ttl)
	s.mu.Unlock()
	return chooseRandomProxy(tier)
}

type provisioner struct {
	cfg      config
	store    *store
	client   *sub2apiClient
	selector *proxySelector
	owner    string
}

func (p *provisioner) run(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.pollInterval)
	defer ticker.Stop()
	for {
		p.processAvailable(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *provisioner) processAvailable(ctx context.Context) {
	for {
		claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		item, err := p.store.claim(claimCtx, p.owner, p.cfg.provisionTimeout)
		cancel()
		if err != nil {
			slog.Error("heartbeat job claim failed", "error", safeError(err))
			return
		}
		if item == nil {
			return
		}
		p.process(ctx, item)
	}
}

func (p *provisioner) process(parent context.Context, item *job) {
	ctx, cancel := context.WithTimeout(parent, p.cfg.provisionTimeout)
	defer cancel()
	if err := p.provision(ctx, item); err != nil {
		if item.AccountID.Valid {
			_ = p.client.updateAccount(context.Background(), item.AccountID.Int64, map[string]any{"status": "inactive"})
		}
		if retryErr := p.store.retry(context.Background(), item, err, p.cfg.maxAttempts); retryErr != nil {
			slog.Error("heartbeat job retry update failed", "job_id", item.ID, "error", safeError(retryErr))
		}
		return
	}
	if err := p.store.complete(context.Background(), item.ID); err != nil {
		slog.Error("heartbeat job completion update failed", "job_id", item.ID, "error", safeError(err))
	}
}

func (p *provisioner) provision(ctx context.Context, item *job) error {
	var accountID int64
	if item.AccountID.Valid {
		accountID = item.AccountID.Int64
		proxyID, err := p.selector.choose(ctx)
		if err != nil {
			return err
		}
		if err := p.client.updateAccount(ctx, accountID, map[string]any{"proxy_id": proxyID}); err != nil {
			return err
		}
		if err := p.store.setProxy(ctx, item.ID, proxyID); err != nil {
			return err
		}
	} else {
		proxyID := int64(0)
		if item.ProxyID.Valid {
			proxyID = item.ProxyID.Int64
		} else {
			var err error
			proxyID, err = p.selector.choose(ctx)
			if err != nil {
				return err
			}
			if err := p.store.setProxy(ctx, item.ID, proxyID); err != nil {
				return err
			}
		}
		key, err := fetchVaultKey(ctx, p.cfg, item.Fingerprint)
		if err != nil {
			return err
		}
		accountID, err = p.client.createUnassignedDeepSeekAccount(ctx, item.Fingerprint, key, proxyID)
		if err != nil {
			return err
		}
		if err := p.store.setAccount(ctx, item.ID, accountID); err != nil {
			return err
		}
		item.AccountID = sql.NullInt64{Int64: accountID, Valid: true}
	}
	if err := p.client.verifyDeepSeekBalance(ctx, accountID); err != nil {
		return err
	}
	return p.client.updateAccount(ctx, accountID, map[string]any{"group_ids": []int64{p.cfg.deepSeekGroupID}, "status": "active"})
}

type heartbeatServer struct {
	cfg         config
	store       *store
	requestMu   sync.Mutex
	lastRequest time.Time
}

func (s *heartbeatServer) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *heartbeatServer) heartbeat(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, maxHeartbeatBodyBytes)
	defer request.Body.Close()
	var payload heartbeatRequest
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		http.Error(w, "invalid heartbeat", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(payload.SessionKey), []byte(s.cfg.allowedSessionKey)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if payload.Timestamp <= 0 || time.Since(time.Unix(payload.Timestamp, 0)).Abs() > maxClockSkew {
		http.Error(w, "invalid timestamp", http.StatusBadRequest)
		return
	}
	s.requestMu.Lock()
	tooSoon := !s.lastRequest.IsZero() && time.Since(s.lastRequest) < minHeartbeatInterval
	if !tooSoon {
		s.lastRequest = time.Now()
	}
	s.requestMu.Unlock()
	if tooSoon {
		http.Error(w, "heartbeat rate limited", http.StatusTooManyRequests)
		return
	}
	if len(payload.Keys) == 0 || len(payload.Keys) > maxHeartbeatKeys {
		http.Error(w, "invalid key list", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	hash := sessionHash(payload.SessionKey)
	accepted := 0
	for _, key := range payload.Keys {
		if !strings.EqualFold(strings.TrimSpace(key.Provider), "ds") || !validFingerprint(key.Fingerprint) {
			continue
		}
		key.Fingerprint = strings.ToLower(key.Fingerprint)
		if err := s.store.enqueue(ctx, key, hash); err != nil {
			slog.Error("heartbeat enqueue failed", "fingerprint", key.Fingerprint, "error", safeError(err))
			http.Error(w, "queue unavailable", http.StatusServiceUnavailable)
			return
		}
		accepted++
	}
	if accepted == 0 {
		http.Error(w, "no supported keys", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(fmt.Sprintf(`{"accepted":%d}`, accepted)))
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("invalid heartbeat bridge configuration", "error", safeError(err))
		os.Exit(1)
	}
	database, err := sql.Open("postgres", cfg.databaseURL)
	if err != nil {
		slog.Error("open heartbeat database", "error", safeError(err))
		os.Exit(1)
	}
	database.SetMaxOpenConns(cfg.workerCount + 4)
	database.SetMaxIdleConns(cfg.workerCount + 2)
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		slog.Error("connect heartbeat database", "error", safeError(err))
		os.Exit(1)
	}
	jobStore := &store{db: database}
	if err := jobStore.migrate(ctx); err != nil {
		slog.Error("migrate heartbeat database", "error", safeError(err))
		os.Exit(1)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	apiClient := newSub2APIClient(cfg)
	preflightCtx, preflightCancel := context.WithTimeout(rootCtx, 15*time.Second)
	if err := apiClient.ensureNoDefaultDeepSeekGroup(preflightCtx); err != nil {
		preflightCancel()
		slog.Error("heartbeat bridge preflight failed", "error", safeError(err))
		return
	}
	if err := apiClient.validateDeepSeekGroup(preflightCtx, cfg.deepSeekGroupID); err != nil {
		preflightCancel()
		slog.Error("heartbeat bridge preflight failed", "error", safeError(err))
		return
	}
	preflightCancel()
	selector := &proxySelector{
		client:  apiClient,
		groupID: cfg.proxyGroupID,
		workers: cfg.proxyProbeWorkers,
		timeout: cfg.proxyProbeTimeout,
		ttl:     cfg.proxySweepTTL,
	}
	for i := 0; i < cfg.workerCount; i++ {
		worker := &provisioner{cfg: cfg, store: jobStore, client: apiClient, selector: selector, owner: fmt.Sprintf("%s-%d", hostname(), i)}
		go worker.run(rootCtx)
	}
	server := &http.Server{
		Addr:              cfg.listenAddr,
		Handler:           routes(&heartbeatServer{cfg: cfg, store: jobStore}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		slog.Info("heartbeat bridge started", "address", cfg.listenAddr, "proxy_group_id", cfg.proxyGroupID, "deepseek_group_id", cfg.deepSeekGroupID)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("heartbeat bridge failed", "error", safeError(err))
			stop()
		}
	}()
	<-rootCtx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}

func routes(server *heartbeatServer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", server.health)
	mux.HandleFunc("POST /api/heartbeat", server.heartbeat)
	return mux
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "heartbeat-worker"
	}
	return name
}
