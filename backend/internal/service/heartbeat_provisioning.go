package service

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/big"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	heartbeatMaxKeys      = 100
	heartbeatMaxBodyBytes = 1 << 20
	heartbeatMaxClockSkew = 15 * time.Minute
	heartbeatLease        = 20 * time.Minute
	heartbeatMinInterval  = 10 * time.Second
)

var (
	ErrHeartbeatDisabled       = errors.New("heartbeat provisioning is disabled")
	ErrHeartbeatUnauthorized   = errors.New("heartbeat source is not allowed")
	ErrHeartbeatInvalidPayload = errors.New("invalid heartbeat payload")
	ErrHeartbeatRateLimited    = errors.New("heartbeat intake is rate limited")
)

type HeartbeatKeyInput struct {
	Fingerprint string
	Provider    string
	Balance     float64
	CheckedAt   time.Time
}

type HeartbeatProvisioningJob struct {
	ID                   int64
	Fingerprint          string
	SessionKeyCiphertext string
	Attempts             int
	AccountID            *int64
	ProxyID              *int64
}

type HeartbeatProvisioningEnqueueInput struct {
	Fingerprint          string
	SessionKeyCiphertext string
	SourceBalance        float64
	SourceCheckedAt      time.Time
}

type HeartbeatProvisioningRepository interface {
	Enqueue(ctx context.Context, input HeartbeatProvisioningEnqueueInput) error
	Claim(ctx context.Context, workerID string, lease time.Duration) (*HeartbeatProvisioningJob, error)
	SetProxy(ctx context.Context, jobID, proxyID int64) error
	SetAccount(ctx context.Context, jobID, accountID int64) error
	FindPendingAccountByFingerprint(ctx context.Context, fingerprint string) (*int64, error)
	Complete(ctx context.Context, jobID int64) error
	Retry(ctx context.Context, jobID int64, attempts int, availableAt time.Time, terminal bool, lastError string) error
}

type HeartbeatProvisioningService struct {
	cfg       config.HeartbeatProvisioningConfig
	repo      HeartbeatProvisioningRepository
	encryptor SecretEncryptor
	admin     AdminService
	balance   *AccountTestService
	vaultURL  *url.URL
	http      *http.Client
	allowed   map[netip.Addr]struct{}
	workerID  string

	ctx      context.Context
	cancel   context.CancelFunc
	start    sync.Once
	stop     sync.Once
	wg       sync.WaitGroup
	running  atomic.Bool
	rateMu   sync.Mutex
	rateLast time.Time

	proxyMu        sync.RWMutex
	proxyRefreshMu sync.Mutex
	proxyTier      []int64
	proxyExpiresAt time.Time
}

func NewHeartbeatProvisioningService(cfg *config.Config, repo HeartbeatProvisioningRepository, encryptor SecretEncryptor, admin AdminService, balance *AccountTestService) (*HeartbeatProvisioningService, error) {
	ctx, cancel := context.WithCancel(context.Background())
	svc := &HeartbeatProvisioningService{
		cfg:       cfg.HeartbeatProvisioning,
		repo:      repo,
		encryptor: encryptor,
		admin:     admin,
		balance:   balance,
		http:      &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
		allowed:   make(map[netip.Addr]struct{}),
		workerID:  fmt.Sprintf("heartbeat-%d", time.Now().UnixNano()),
		ctx:       ctx,
		cancel:    cancel,
	}
	if !svc.cfg.Enabled {
		return svc, nil
	}
	parsedURL, err := url.Parse(strings.TrimSpace(svc.cfg.VaultURL))
	if err != nil || parsedURL == nil || parsedURL.Host == "" {
		return nil, fmt.Errorf("parse heartbeat vault URL: %w", err)
	}
	if parsedURL.Scheme != "https" && (parsedURL.Scheme != "http" || !svc.cfg.AllowInsecureVault) {
		return nil, errors.New("heartbeat vault must use HTTPS unless insecure vault access is explicitly enabled")
	}
	svc.vaultURL = parsedURL
	for _, raw := range svc.cfg.AllowedSourceIPs {
		addr, err := netip.ParseAddr(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("parse heartbeat source IP: %w", err)
		}
		svc.allowed[addr.Unmap()] = struct{}{}
	}
	preflightCtx, preflightCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer preflightCancel()
	group, err := admin.GetGroup(preflightCtx, svc.cfg.DeepSeekGroupID)
	if err != nil {
		return nil, fmt.Errorf("load heartbeat DeepSeek group: %w", err)
	}
	if group == nil || group.Status != StatusActive || group.Platform != PlatformDeepSeek {
		return nil, errors.New("heartbeat DeepSeek group must be active and use the deepseek platform")
	}
	proxyGroupID := svc.cfg.ProxyGroupID
	_, proxyTotal, err := admin.ListProxies(preflightCtx, 1, 1, ProxyListFilters{Status: StatusActive, ProxyGroupID: &proxyGroupID}, "id", "asc")
	if err != nil {
		return nil, fmt.Errorf("load heartbeat proxy group: %w", err)
	}
	if proxyTotal == 0 {
		return nil, errors.New("heartbeat proxy group has no active proxies")
	}
	return svc, nil
}

func ProvideHeartbeatProvisioningService(cfg *config.Config, repo HeartbeatProvisioningRepository, encryptor SecretEncryptor, admin AdminService, balance *AccountTestService) (*HeartbeatProvisioningService, error) {
	svc, err := NewHeartbeatProvisioningService(cfg, repo, encryptor, admin, balance)
	if err != nil {
		return nil, err
	}
	svc.Start()
	return svc, nil
}

func (s *HeartbeatProvisioningService) Start() {
	if s == nil || !s.cfg.Enabled || s.repo == nil || s.encryptor == nil || s.admin == nil || s.balance == nil {
		return
	}
	s.start.Do(func() {
		s.running.Store(true)
		for i := 0; i < s.cfg.WorkerCount; i++ {
			s.wg.Add(1)
			go s.runWorker(i)
		}
	})
}

func (s *HeartbeatProvisioningService) Stop() {
	if s == nil {
		return
	}
	s.stop.Do(func() {
		s.cancel()
		s.wg.Wait()
		s.running.Store(false)
	})
}

func (s *HeartbeatProvisioningService) Enabled() bool {
	return s != nil && s.cfg.Enabled
}

func (s *HeartbeatProvisioningService) Queue(ctx context.Context, sourceIP, sessionKey string, timestamp time.Time, keys []HeartbeatKeyInput) (int, error) {
	if !s.Enabled() {
		return 0, ErrHeartbeatDisabled
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(sourceIP))
	if err != nil {
		return 0, ErrHeartbeatUnauthorized
	}
	if _, ok := s.allowed[addr.Unmap()]; !ok {
		return 0, ErrHeartbeatUnauthorized
	}
	if !validHeartbeatSessionKey(sessionKey) || timestamp.IsZero() || time.Since(timestamp).Abs() > heartbeatMaxClockSkew || len(keys) == 0 || len(keys) > heartbeatMaxKeys {
		return 0, ErrHeartbeatInvalidPayload
	}

	for _, key := range keys {
		if !strings.EqualFold(strings.TrimSpace(key.Provider), "ds") || !validHeartbeatFingerprint(key.Fingerprint) || key.CheckedAt.IsZero() {
			return 0, ErrHeartbeatInvalidPayload
		}
	}
	ciphertext, err := s.encryptor.Encrypt(sessionKey)
	if err != nil {
		return 0, fmt.Errorf("encrypt heartbeat session key: %w", err)
	}

	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	if !s.rateLast.IsZero() && time.Since(s.rateLast) < heartbeatMinInterval {
		return 0, ErrHeartbeatRateLimited
	}
	accepted := 0
	for _, key := range keys {
		if err := s.repo.Enqueue(ctx, HeartbeatProvisioningEnqueueInput{
			Fingerprint:          strings.ToLower(strings.TrimSpace(key.Fingerprint)),
			SessionKeyCiphertext: ciphertext,
			SourceBalance:        key.Balance,
			SourceCheckedAt:      key.CheckedAt,
		}); err != nil {
			return accepted, err
		}
		accepted++
	}
	s.rateLast = time.Now()
	return accepted, nil
}

func (s *HeartbeatProvisioningService) runWorker(index int) {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := s.processOne(s.ctx, fmt.Sprintf("%s-%d", s.workerID, index)); err != nil && s.ctx.Err() == nil {
			slog.Warn("heartbeat provisioning worker failed", "error", heartbeatSafeError(err))
		}
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *HeartbeatProvisioningService) processOne(ctx context.Context, workerID string) error {
	job, err := s.repo.Claim(ctx, workerID, heartbeatLease)
	if err != nil || job == nil {
		return err
	}
	workCtx, cancel := context.WithTimeout(ctx, heartbeatLease)
	err = s.provision(workCtx, job)
	cancel()
	if err == nil {
		return s.repo.Complete(context.Background(), job.ID)
	}
	if job.AccountID != nil {
		falseValue := false
		_, _ = s.admin.UpdateAccount(context.Background(), *job.AccountID, &UpdateAccountInput{Status: StatusDisabled})
		_, _ = s.admin.SetAccountSchedulable(context.Background(), *job.AccountID, falseValue)
	}
	terminal := job.Attempts >= s.cfg.MaxAttempts
	return s.repo.Retry(context.Background(), job.ID, job.Attempts, time.Now().UTC().Add(heartbeatRetryDelay(job.Attempts)), terminal, heartbeatSafeError(err))
}

func (s *HeartbeatProvisioningService) provision(ctx context.Context, job *HeartbeatProvisioningJob) error {
	if job == nil {
		return errors.New("nil heartbeat job")
	}
	accountID := int64(0)
	if job.AccountID != nil {
		accountID = *job.AccountID
		proxyID, err := s.selectProxy(ctx)
		if err != nil {
			return err
		}
		if _, err := s.admin.UpdateAccount(ctx, accountID, &UpdateAccountInput{ProxyID: &proxyID}); err != nil {
			return err
		}
		if err := s.repo.SetProxy(ctx, job.ID, proxyID); err != nil {
			return err
		}
	} else {
		recoveredID, err := s.repo.FindPendingAccountByFingerprint(ctx, job.Fingerprint)
		if err != nil {
			return err
		}
		if recoveredID != nil {
			if err := s.repo.SetAccount(ctx, job.ID, *recoveredID); err != nil {
				return err
			}
			job.AccountID = recoveredID
			return s.provision(ctx, job)
		}
		proxyID, err := s.selectProxy(ctx)
		if err != nil {
			return err
		}
		if err := s.repo.SetProxy(ctx, job.ID, proxyID); err != nil {
			return err
		}
		sessionKey, err := s.encryptor.Decrypt(job.SessionKeyCiphertext)
		if err != nil {
			return errors.New("decrypt heartbeat session key")
		}
		key, err := s.fetchVaultKey(ctx, sessionKey, job.Fingerprint)
		if err != nil {
			return err
		}
		falseValue := false
		account, err := s.admin.CreateAccount(ctx, &CreateAccountInput{
			Name:                 "heartbeat-ds-" + job.Fingerprint,
			Platform:             PlatformDeepSeek,
			Type:                 AccountTypeAPIKey,
			Credentials:          map[string]any{"api_key": key, "pool_mode": true, "pool_mode_retry_count": 3, "pool_mode_retry_status_codes": []int{401, 403, 429}},
			Extra:                map[string]any{"heartbeat_fp": job.Fingerprint, "heartbeat_source": "key-checker"},
			ProxyID:              &proxyID,
			Concurrency:          3,
			Priority:             50,
			SkipDefaultGroupBind: true,
			InitialStatus:        StatusDisabled,
			InitialSchedulable:   &falseValue,
		})
		if err != nil {
			return err
		}
		accountID = account.ID
		if err := s.repo.SetAccount(ctx, job.ID, accountID); err != nil {
			return err
		}
		job.AccountID = &accountID
	}
	balance, err := s.balance.FetchDeepSeekBalance(ctx, accountID)
	if err != nil {
		return err
	}
	if !balance.IsAvailable {
		return errors.New("DeepSeek balance is unavailable")
	}
	groupIDs := []int64{s.cfg.DeepSeekGroupID}
	if _, err := s.admin.UpdateAccount(ctx, accountID, &UpdateAccountInput{GroupIDs: &groupIDs}); err != nil {
		return err
	}
	if _, err := s.admin.UpdateAccount(ctx, accountID, &UpdateAccountInput{Status: StatusActive}); err != nil {
		return err
	}
	_, err = s.admin.SetAccountSchedulable(ctx, accountID, true)
	return err
}

func (s *HeartbeatProvisioningService) selectProxy(ctx context.Context) (int64, error) {
	now := time.Now()
	s.proxyMu.RLock()
	cached := append([]int64(nil), s.proxyTier...)
	valid := len(cached) > 0 && now.Before(s.proxyExpiresAt)
	s.proxyMu.RUnlock()
	if valid {
		return chooseHeartbeatProxy(cached)
	}
	s.proxyRefreshMu.Lock()
	defer s.proxyRefreshMu.Unlock()
	s.proxyMu.RLock()
	cached = append([]int64(nil), s.proxyTier...)
	valid = len(cached) > 0 && time.Now().Before(s.proxyExpiresAt)
	s.proxyMu.RUnlock()
	if valid {
		return chooseHeartbeatProxy(cached)
	}
	groupID := s.cfg.ProxyGroupID
	proxies := make([]Proxy, 0)
	for page := 1; ; page++ {
		batch, total, err := s.admin.ListProxies(ctx, page, 500, ProxyListFilters{Status: StatusActive, ProxyGroupID: &groupID}, "id", "asc")
		if err != nil {
			return 0, err
		}
		proxies = append(proxies, batch...)
		if len(batch) == 0 || int64(len(proxies)) >= total {
			break
		}
	}
	if len(proxies) == 0 {
		return 0, errors.New("no active proxies in heartbeat target group")
	}
	if len(proxies) > s.cfg.ProxyProbeSampleSize {
		sampled, err := sampleHeartbeatProxies(proxies, s.cfg.ProxyProbeSampleSize)
		if err != nil {
			return 0, err
		}
		proxies = sampled
	}
	type probeResult struct{ id, latency int64 }
	input := make(chan Proxy)
	output := make(chan probeResult, len(proxies))
	var wg sync.WaitGroup
	for i := 0; i < s.cfg.ProxyProbeWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for proxy := range input {
				probeCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.ProxyProbeTimeoutS)*time.Second)
				result, err := s.admin.TestProxy(probeCtx, proxy.ID)
				cancel()
				if err == nil && result != nil && result.Success && result.LatencyMs > 0 {
					output <- probeResult{id: proxy.ID, latency: result.LatencyMs}
				}
			}
		}()
	}
	go func() {
		defer close(input)
		for _, proxy := range proxies {
			select {
			case input <- proxy:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(output) }()
	results := make([]probeResult, 0, len(proxies))
	for result := range output {
		results = append(results, result)
	}
	if len(results) == 0 {
		return 0, errors.New("no heartbeat proxy completed a successful latency probe")
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
	s.proxyMu.Lock()
	s.proxyTier = append([]int64(nil), tier...)
	s.proxyExpiresAt = time.Now().Add(time.Duration(s.cfg.ProxySweepTTLSecond) * time.Second)
	s.proxyMu.Unlock()
	return chooseHeartbeatProxy(tier)
}

func (s *HeartbeatProvisioningService) fetchVaultKey(ctx context.Context, sessionKey, fingerprint string) (string, error) {
	u := *s.vaultURL
	query := u.Query()
	query.Set("session_key", sessionKey)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", errors.New("create heartbeat vault request")
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return "", errors.New("heartbeat vault request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("heartbeat vault returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, heartbeatMaxBodyBytes+1))
	if err != nil || len(body) > heartbeatMaxBodyBytes {
		return "", errors.New("invalid heartbeat vault response")
	}
	var payload struct {
		OK   bool `json:"ok"`
		Keys []struct {
			Key      string `json:"key"`
			Provider string `json:"provider"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || !payload.OK {
		return "", errors.New("invalid heartbeat vault payload")
	}
	for _, candidate := range payload.Keys {
		if strings.EqualFold(strings.TrimSpace(candidate.Provider), "ds") && heartbeatFingerprint(candidate.Key) == jobFingerprint(fingerprint) {
			return strings.TrimSpace(candidate.Key), nil
		}
	}
	return "", errors.New("matching DeepSeek key is not present in heartbeat vault")
}

func chooseHeartbeatProxy(candidates []int64) (int64, error) {
	if len(candidates) == 0 {
		return 0, errors.New("no low-latency heartbeat proxy candidates")
	}
	choice, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(len(candidates))))
	if err != nil {
		return 0, err
	}
	return candidates[choice.Int64()], nil
}

func sampleHeartbeatProxies(proxies []Proxy, count int) ([]Proxy, error) {
	if count <= 0 || count >= len(proxies) {
		return append([]Proxy(nil), proxies...), nil
	}
	sampled := append([]Proxy(nil), proxies...)
	for i := len(sampled) - 1; i > 0; i-- {
		choice, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return nil, err
		}
		j := int(choice.Int64())
		sampled[i], sampled[j] = sampled[j], sampled[i]
	}
	return sampled[:count], nil
}

func heartbeatFingerprint(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])[:24]
}

func jobFingerprint(fingerprint string) string {
	return strings.ToLower(strings.TrimSpace(fingerprint))
}

func validHeartbeatFingerprint(value string) bool {
	if len(strings.TrimSpace(value)) != 24 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validHeartbeatSessionKey(value string) bool {
	if len(strings.TrimSpace(value)) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func heartbeatRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	seconds := math.Min(30*math.Pow(2, float64(attempt-1)), 1800)
	return time.Duration(seconds) * time.Second
}

func heartbeatSafeError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.NewReplacer("sk-", "[redacted]-", "session_key", "session").Replace(err.Error())
	if len(message) > 400 {
		return message[:400]
	}
	return message
}
