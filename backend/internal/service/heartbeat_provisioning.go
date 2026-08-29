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
	heartbeatSettingKey   = SettingKeyHeartbeatProvisioningConfig
)

var (
	ErrHeartbeatDisabled       = errors.New("heartbeat provisioning is disabled")
	ErrHeartbeatUnauthorized   = errors.New("heartbeat source is not allowed")
	ErrHeartbeatInvalidPayload = errors.New("invalid heartbeat payload")
	ErrHeartbeatRateLimited    = errors.New("heartbeat intake is rate limited")
)

// heartbeatProviderSpec is the wire-to-account-platform contract for the
// external key checker. The short "ds" identifier is retained for backwards
// compatibility with existing heartbeat jobs and Vault responses.
type heartbeatProviderSpec struct {
	ID       string
	Platform string
	Aliases  []string
}

var heartbeatProviderRegistry = []heartbeatProviderSpec{
	{ID: "ds", Platform: PlatformDeepSeek, Aliases: []string{"ds", "deepseek"}},
	{ID: PlatformAnthropic, Platform: PlatformAnthropic, Aliases: []string{"anthropic", "claude"}},
	{ID: PlatformOpenAI, Platform: PlatformOpenAI, Aliases: []string{"openai", "gpt"}},
	// These external checker IDs use the OpenAI-compatible account runtime while
	// remaining distinct wire IDs for Vault matching and job persistence.
	{ID: "toapis", Platform: PlatformOpenAI, Aliases: []string{"toapis"}},
	{ID: "kling", Platform: PlatformOpenAI, Aliases: []string{"kling"}},
	{ID: "sf", Platform: PlatformOpenAI, Aliases: []string{"sf", "siliconflow"}},
	{ID: "mimo", Platform: PlatformOpenAI, Aliases: []string{"mimo"}},
	{ID: "groq", Platform: PlatformOpenAI, Aliases: []string{"groq"}},
	{ID: "perplexity", Platform: PlatformOpenAI, Aliases: []string{"perplexity"}},
	{ID: PlatformGemini, Platform: PlatformGemini, Aliases: []string{"gemini", "google"}},
	{ID: PlatformAntigravity, Platform: PlatformAntigravity, Aliases: []string{"antigravity"}},
	{ID: PlatformGrok, Platform: PlatformGrok, Aliases: []string{"grok", "xai"}},
	{ID: PlatformAgnes, Platform: PlatformAgnes, Aliases: []string{"agnes"}},
	{ID: PlatformNvidia, Platform: PlatformNvidia, Aliases: []string{"nvidia"}},
	{ID: PlatformTokenRhythm, Platform: PlatformTokenRhythm, Aliases: []string{"tokenrhythm", "token_rhythm", "tr"}},
	{ID: PlatformKimi, Platform: PlatformKimi, Aliases: []string{"kimi", "moonshot"}},
	{ID: PlatformZhipu, Platform: PlatformZhipu, Aliases: []string{"zhipu", "zhipuai", "bigmodel"}},
	{ID: PlatformChatAnywhere, Platform: PlatformChatAnywhere, Aliases: []string{"chatanywhere"}},
	{ID: PlatformGLM, Platform: PlatformGLM, Aliases: []string{"glm"}},
	{ID: PlatformModelScope, Platform: PlatformModelScope, Aliases: []string{"modelscope"}},
	{ID: PlatformDashScope, Platform: PlatformDashScope, Aliases: []string{"dashscope", "aliyun", "qwen"}},
	{ID: "bailian_sp", Platform: PlatformDashScope, Aliases: []string{"bailian_sp"}},
	{ID: PlatformMiniMax, Platform: PlatformMiniMax, Aliases: []string{"minimax"}},
	{ID: PlatformVolcengine, Platform: PlatformVolcengine, Aliases: []string{"volcengine", "ark", "doubao"}},
}

func normalizeHeartbeatProvider(raw string) (heartbeatProviderSpec, bool) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	for _, spec := range heartbeatProviderRegistry {
		for _, alias := range spec.Aliases {
			if normalized == alias {
				return spec, true
			}
		}
	}
	return heartbeatProviderSpec{}, false
}

func heartbeatProviderForPlatform(platform string) (heartbeatProviderSpec, bool) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	for _, spec := range heartbeatProviderRegistry {
		if spec.Platform == platform {
			return spec, true
		}
	}
	return heartbeatProviderSpec{}, false
}

func isHeartbeatTargetGroupPlatform(platform string) bool {
	if strings.EqualFold(strings.TrimSpace(platform), PlatformComposite) {
		return true
	}
	_, supported := heartbeatProviderForPlatform(platform)
	return supported
}

func heartbeatAccountCanUseGroup(groupPlatform, accountPlatform string) bool {
	if strings.EqualFold(strings.TrimSpace(groupPlatform), PlatformComposite) {
		return true
	}
	return accountPlatformMatchesGroup(groupPlatform, accountPlatform)
}

// HeartbeatProviderPlatform resolves a heartbeat wire provider to the account
// platform used by the provisioning worker. It is exported for repository
// queries that need to scope recovery by provider platform.
func HeartbeatProviderPlatform(provider string) (string, bool) {
	spec, ok := normalizeHeartbeatProvider(provider)
	if !ok {
		return "", false
	}
	return spec.Platform, true
}

// HeartbeatProviderID resolves an incoming provider alias to the stable ID
// persisted in heartbeat jobs and account metadata.
func HeartbeatProviderID(provider string) (string, bool) {
	spec, ok := normalizeHeartbeatProvider(provider)
	if !ok {
		return "", false
	}
	return spec.ID, true
}

type HeartbeatKeyInput struct {
	Fingerprint string
	Provider    string
	Balance     float64
	CheckedAt   time.Time
	GroupID     *int64
}

type HeartbeatProvisioningJob struct {
	ID                   int64
	Provider             string
	Fingerprint          string
	SessionKeyCiphertext string
	Attempts             int
	TargetGroupID        int64
	TargetProxyGroupID   int64
	AccountID            *int64
	ProxyID              *int64
}

type HeartbeatProvisioningEnqueueInput struct {
	Provider             string
	Fingerprint          string
	SessionKeyCiphertext string
	SourceBalance        float64
	SourceCheckedAt      time.Time
	TargetGroupID        int64
	TargetProxyGroupID   int64
}

type HeartbeatQueueStats struct {
	Queued      int64
	Processing  int64
	Retry       int64
	Failed      int64
	Complete    int64
	LastError   string
	LastErrorAt *time.Time
}

type HeartbeatProvisioningStatus struct {
	Enabled         bool       `json:"enabled"`
	Running         bool       `json:"running"`
	ConfigSource    string     `json:"config_source"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	Queued          int64      `json:"queued"`
	Processing      int64      `json:"processing"`
	Retry           int64      `json:"retry"`
	Failed          int64      `json:"failed"`
	Complete        int64      `json:"complete"`
	LastError       string     `json:"last_error,omitempty"`
	LastErrorAt     *time.Time `json:"last_error_at,omitempty"`
}

type heartbeatVaultCredential struct {
	Key         string
	Credentials map[string]any
}

type HeartbeatGroupOption struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Status   string `json:"status"`
}

type HeartbeatProxyGroupOption struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	ActiveProxyCount int64  `json:"active_proxy_count"`
}

type HeartbeatProvisioningOptions struct {
	Groups      []HeartbeatGroupOption      `json:"groups"`
	ProxyGroups []HeartbeatProxyGroupOption `json:"proxy_groups"`
}

type HeartbeatProvisioningRepository interface {
	Enqueue(ctx context.Context, input HeartbeatProvisioningEnqueueInput) error
	Claim(ctx context.Context, workerID string, lease time.Duration) (*HeartbeatProvisioningJob, error)
	SetProxy(ctx context.Context, jobID, proxyID int64) error
	SetAccount(ctx context.Context, jobID, accountID int64) error
	FindPendingAccountByFingerprint(ctx context.Context, fingerprint string) (*int64, error)
	Complete(ctx context.Context, jobID int64) error
	Retry(ctx context.Context, jobID int64, attempts int, availableAt time.Time, terminal bool, lastError string) error
	Stats(ctx context.Context) (*HeartbeatQueueStats, error)
}

type heartbeatProviderRecoveryRepository interface {
	FindPendingAccountByProviderAndFingerprint(ctx context.Context, provider, fingerprint string) (*int64, error)
}

type heartbeatPreparedConfig struct {
	vaultURL *url.URL
	allowed  map[netip.Addr]struct{}
}

type heartbeatProxyTier struct {
	ids       []int64
	expiresAt time.Time
}

type HeartbeatProvisioningService struct {
	cfg       config.HeartbeatProvisioningConfig
	source    string
	repo      HeartbeatProvisioningRepository
	settings  SettingRepository
	encryptor SecretEncryptor
	admin     AdminService
	balance   *AccountTestService
	http      *http.Client
	vaultURL  *url.URL
	allowed   map[netip.Addr]struct{}
	workerID  string

	cfgMu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc

	lifecycleMu  sync.Mutex
	started      bool
	stopped      bool
	workerCancel context.CancelFunc
	wg           sync.WaitGroup
	running      atomic.Bool

	updateMu sync.Mutex
	rateMu   sync.Mutex
	rateLast time.Time

	lastHeartbeatNS atomic.Int64

	proxyMu        sync.RWMutex
	proxyRefreshMu sync.Mutex
	proxyTiers     map[int64]heartbeatProxyTier
}

func NewHeartbeatProvisioningService(cfg *config.Config, repo HeartbeatProvisioningRepository, encryptor SecretEncryptor, admin AdminService, balance *AccountTestService, settings SettingRepository) (*HeartbeatProvisioningService, error) {
	if cfg == nil {
		return nil, errors.New("nil heartbeat configuration")
	}
	ctx, cancel := context.WithCancel(context.Background())
	svc := &HeartbeatProvisioningService{
		cfg:        normalizeHeartbeatConfig(cfg.HeartbeatProvisioning),
		source:     "deployment",
		repo:       repo,
		settings:   settings,
		encryptor:  encryptor,
		admin:      admin,
		balance:    balance,
		http:       &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
		workerID:   fmt.Sprintf("heartbeat-%d", time.Now().UnixNano()),
		ctx:        ctx,
		cancel:     cancel,
		proxyTiers: make(map[int64]heartbeatProxyTier),
	}

	if settings != nil {
		stored, err := settings.GetValue(context.Background(), heartbeatSettingKey)
		if err == nil {
			storedConfig, decodeErr := decodeStoredHeartbeatConfig(stored)
			if decodeErr != nil {
				return nil, fmt.Errorf("decode stored heartbeat configuration: %w", decodeErr)
			}
			svc.cfg = normalizeHeartbeatConfig(storedConfig)
			svc.source = "database"
		} else if !errors.Is(err, ErrSettingNotFound) {
			return nil, fmt.Errorf("load stored heartbeat configuration: %w", err)
		}
	}

	prepared, err := svc.prepareConfig(context.Background(), svc.cfg)
	if err != nil {
		cancel()
		return nil, err
	}
	svc.vaultURL = prepared.vaultURL
	svc.allowed = prepared.allowed
	return svc, nil
}

func ProvideHeartbeatProvisioningService(cfg *config.Config, repo HeartbeatProvisioningRepository, encryptor SecretEncryptor, admin AdminService, balance *AccountTestService, settings SettingRepository) (*HeartbeatProvisioningService, error) {
	svc, err := NewHeartbeatProvisioningService(cfg, repo, encryptor, admin, balance, settings)
	if err != nil {
		return nil, err
	}
	svc.Start()
	return svc, nil
}

func (s *HeartbeatProvisioningService) Start() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.stopped || s.started {
		return
	}
	s.started = true
	s.startWorkersLocked()
}

func (s *HeartbeatProvisioningService) startWorkersLocked() {
	if !s.enabledSnapshot() || s.workerCancel != nil || s.repo == nil || s.encryptor == nil || s.admin == nil || s.balance == nil {
		return
	}
	cfg := s.configSnapshot()
	workerCtx, cancel := context.WithCancel(s.ctx)
	s.workerCancel = cancel
	s.running.Store(true)
	for i := 0; i < cfg.WorkerCount; i++ {
		s.wg.Add(1)
		go s.runWorker(workerCtx, i)
	}
}

func (s *HeartbeatProvisioningService) stopWorkers() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	cancel := s.workerCancel
	s.workerCancel = nil
	s.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
	s.running.Store(false)
}

func (s *HeartbeatProvisioningService) Stop() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	if s.stopped {
		s.lifecycleMu.Unlock()
		return
	}
	s.stopped = true
	s.started = false
	cancel := s.workerCancel
	s.workerCancel = nil
	s.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.cancel()
	s.wg.Wait()
	s.running.Store(false)
}

func (s *HeartbeatProvisioningService) Enabled() bool {
	return s != nil && s.enabledSnapshot()
}

func (s *HeartbeatProvisioningService) Running() bool {
	return s != nil && s.running.Load()
}

func (s *HeartbeatProvisioningService) ConfigSnapshot() (config.HeartbeatProvisioningConfig, string) {
	if s == nil {
		return config.HeartbeatProvisioningConfig{}, ""
	}
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return cloneHeartbeatConfig(s.cfg), s.source
}

func (s *HeartbeatProvisioningService) UpdateConfig(ctx context.Context, requested config.HeartbeatProvisioningConfig) error {
	if s == nil {
		return errors.New("nil heartbeat provisioning service")
	}
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	normalized := normalizeHeartbeatConfig(requested)
	if err := validateHeartbeatRuntimeConfig(normalized); err != nil {
		return err
	}
	prepared, err := s.prepareConfig(ctx, normalized)
	if err != nil {
		return err
	}
	if s.settings != nil {
		encoded, encodeErr := encodeStoredHeartbeatConfig(normalized)
		if encodeErr != nil {
			return encodeErr
		}
		if err := s.settings.Set(ctx, heartbeatSettingKey, encoded); err != nil {
			return fmt.Errorf("persist heartbeat configuration: %w", err)
		}
	}
	s.cfgMu.Lock()
	s.cfg = normalized
	s.vaultURL = prepared.vaultURL
	s.allowed = prepared.allowed
	s.source = "database"
	s.cfgMu.Unlock()
	s.clearProxyCache()
	s.restartWorkers()
	return nil
}

func (s *HeartbeatProvisioningService) restartWorkers() {
	s.stopWorkers()
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if !s.stopped && s.started {
		s.startWorkersLocked()
	}
}

func (s *HeartbeatProvisioningService) Queue(ctx context.Context, sourceIP, sessionKey string, timestamp time.Time, keys []HeartbeatKeyInput) (int, error) {
	cfg, allowed, enabled := s.runtimeSnapshot()
	if !enabled {
		return 0, ErrHeartbeatDisabled
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(sourceIP))
	if err != nil {
		return 0, ErrHeartbeatUnauthorized
	}
	if _, ok := allowed[addr.Unmap()]; !ok {
		return 0, ErrHeartbeatUnauthorized
	}
	if !validHeartbeatSessionKey(sessionKey) || timestamp.IsZero() || time.Since(timestamp).Abs() > heartbeatMaxClockSkew || len(keys) == 0 || len(keys) > heartbeatMaxKeys {
		return 0, ErrHeartbeatInvalidPayload
	}

	targets := make([]config.HeartbeatProvisioningTarget, len(keys))
	providers := make([]heartbeatProviderSpec, len(keys))
	resolvedTargets := make(map[string]config.HeartbeatProvisioningTarget, len(keys))
	for index, key := range keys {
		provider, providerOK := normalizeHeartbeatProvider(key.Provider)
		if !providerOK || !validHeartbeatFingerprint(key.Fingerprint) || key.CheckedAt.IsZero() {
			return 0, ErrHeartbeatInvalidPayload
		}
		requestedGroupID := int64(0)
		if key.GroupID != nil {
			requestedGroupID = *key.GroupID
		}
		cacheKey := fmt.Sprintf("%s:%d", provider.ID, requestedGroupID)
		target, cached := resolvedTargets[cacheKey]
		if !cached {
			var targetOK bool
			target, targetOK = s.resolveHeartbeatTargetForProvider(ctx, cfg, provider.Platform, key.GroupID)
			if !targetOK {
				return 0, ErrHeartbeatInvalidPayload
			}
			resolvedTargets[cacheKey] = target
		}
		providers[index] = provider
		targets[index] = target
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
	for index, key := range keys {
		if err := s.repo.Enqueue(ctx, HeartbeatProvisioningEnqueueInput{
			Provider:             providers[index].ID,
			Fingerprint:          strings.ToLower(strings.TrimSpace(key.Fingerprint)),
			SessionKeyCiphertext: ciphertext,
			SourceBalance:        key.Balance,
			SourceCheckedAt:      key.CheckedAt,
			TargetGroupID:        targets[index].GroupID,
			TargetProxyGroupID:   targets[index].ProxyGroupID,
		}); err != nil {
			return accepted, err
		}
		accepted++
	}
	s.rateLast = time.Now()
	s.lastHeartbeatNS.Store(time.Now().UTC().UnixNano())
	return accepted, nil
}

func (s *HeartbeatProvisioningService) resolveHeartbeatTargetForProvider(ctx context.Context, cfg config.HeartbeatProvisioningConfig, platform string, requestedGroupID *int64) (config.HeartbeatProvisioningTarget, bool) {
	target, ok := resolveHeartbeatTarget(cfg, requestedGroupID)
	if !ok || s == nil || s.admin == nil {
		return target, ok
	}
	if requestedGroupID != nil {
		return target, s.heartbeatGroupAcceptsPlatform(ctx, target.GroupID, platform)
	}
	// Prefer a target whose concrete group platform matches the provider. This
	// keeps omitted group_id entries intuitive when multiple platform groups are
	// configured, while the default/shared compatible group remains a fallback.
	for _, candidate := range cfg.Targets {
		if s.heartbeatGroupHasPlatform(ctx, candidate.GroupID, platform) {
			return candidate, true
		}
	}
	if s.heartbeatGroupAcceptsPlatform(ctx, target.GroupID, platform) {
		return target, true
	}
	for _, candidate := range cfg.Targets {
		if candidate.GroupID == target.GroupID {
			continue
		}
		if s.heartbeatGroupAcceptsPlatform(ctx, candidate.GroupID, platform) {
			return candidate, true
		}
	}
	return config.HeartbeatProvisioningTarget{}, false
}

func (s *HeartbeatProvisioningService) heartbeatGroupAcceptsPlatform(ctx context.Context, groupID int64, platform string) bool {
	group, err := s.admin.GetGroup(ctx, groupID)
	return err == nil && group != nil && group.Status == StatusActive && heartbeatAccountCanUseGroup(group.Platform, platform)
}

func (s *HeartbeatProvisioningService) heartbeatGroupHasPlatform(ctx context.Context, groupID int64, platform string) bool {
	group, err := s.admin.GetGroup(ctx, groupID)
	return err == nil && group != nil && group.Status == StatusActive && strings.EqualFold(strings.TrimSpace(group.Platform), strings.TrimSpace(platform))
}

func (s *HeartbeatProvisioningService) runWorker(ctx context.Context, index int) {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := s.processOne(ctx, fmt.Sprintf("%s-%d", s.workerID, index)); err != nil && ctx.Err() == nil {
			slog.Warn("heartbeat provisioning worker failed", "error", heartbeatSafeError(err))
		}
		select {
		case <-ctx.Done():
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
	cfg := s.configSnapshot()
	terminal := job.Attempts >= cfg.MaxAttempts
	return s.repo.Retry(context.Background(), job.ID, job.Attempts, time.Now().UTC().Add(heartbeatRetryDelay(job.Attempts)), terminal, heartbeatSafeError(err))
}

func (s *HeartbeatProvisioningService) findPendingAccountByFingerprint(ctx context.Context, provider, fingerprint string) (*int64, error) {
	if repository, ok := s.repo.(heartbeatProviderRecoveryRepository); ok {
		return repository.FindPendingAccountByProviderAndFingerprint(ctx, provider, fingerprint)
	}
	// Legacy repository implementations only know the original DeepSeek
	// heartbeat shape. Keep that fallback for existing deployments; new
	// providers always use the provider-aware query above.
	if provider != "ds" {
		return nil, nil
	}
	return s.repo.FindPendingAccountByFingerprint(ctx, fingerprint)
}

func (s *HeartbeatProvisioningService) provision(ctx context.Context, job *HeartbeatProvisioningJob) error {
	if job == nil {
		return errors.New("nil heartbeat job")
	}
	provider, ok := normalizeHeartbeatProvider(job.Provider)
	if strings.TrimSpace(job.Provider) == "" {
		// Jobs written before provider became explicit were all DeepSeek jobs.
		provider, _ = normalizeHeartbeatProvider("ds")
	} else if !ok {
		return fmt.Errorf("unsupported heartbeat provider %q", job.Provider)
	}
	cfg := s.configSnapshot()
	target, ok := resolveHeartbeatJobTarget(cfg, job)
	if !ok {
		return errors.New("heartbeat job target is not configured")
	}
	group, err := s.admin.GetGroup(ctx, target.GroupID)
	if err != nil {
		return fmt.Errorf("load heartbeat target group %d: %w", target.GroupID, err)
	}
	if group == nil || group.Status != StatusActive || !heartbeatAccountCanUseGroup(group.Platform, provider.Platform) {
		return fmt.Errorf("heartbeat provider %s does not match target group %d platform", provider.ID, target.GroupID)
	}
	accountID := int64(0)
	if job.AccountID != nil {
		accountID = *job.AccountID
		proxyID, err := s.selectProxy(ctx, target.ProxyGroupID, cfg)
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
		recoveredID, err := s.findPendingAccountByFingerprint(ctx, provider.ID, job.Fingerprint)
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
		proxyID, err := s.selectProxy(ctx, target.ProxyGroupID, cfg)
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
		vaultCredential, err := s.fetchVaultCredential(ctx, sessionKey, job.Fingerprint, provider.ID)
		if err != nil {
			return err
		}
		credentials := heartbeatAccountCredentials(provider.ID, vaultCredential)
		falseValue := false
		account, err := s.admin.CreateAccount(ctx, &CreateAccountInput{
			Name:                 "heartbeat-" + provider.ID + "-" + job.Fingerprint,
			Platform:             provider.Platform,
			Type:                 AccountTypeAPIKey,
			Credentials:          credentials,
			Extra:                map[string]any{"heartbeat_fp": job.Fingerprint, "heartbeat_provider": provider.ID, "heartbeat_source": "key-checker"},
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
	if err := s.balance.ValidateHeartbeatAccount(ctx, accountID, provider.Platform); err != nil {
		return err
	}
	groupIDs := []int64{target.GroupID}
	if _, err := s.admin.UpdateAccount(ctx, accountID, &UpdateAccountInput{GroupIDs: &groupIDs}); err != nil {
		return err
	}
	if _, err := s.admin.UpdateAccount(ctx, accountID, &UpdateAccountInput{Status: StatusActive}); err != nil {
		return err
	}
	_, err = s.admin.SetAccountSchedulable(ctx, accountID, true)
	return err
}

func (s *HeartbeatProvisioningService) selectProxy(ctx context.Context, proxyGroupID int64, cfg config.HeartbeatProvisioningConfig) (int64, error) {
	now := time.Now()
	s.proxyMu.RLock()
	cached, valid := s.proxyTiers[proxyGroupID]
	s.proxyMu.RUnlock()
	if valid && len(cached.ids) > 0 && now.Before(cached.expiresAt) {
		return chooseHeartbeatProxy(cached.ids)
	}
	s.proxyRefreshMu.Lock()
	defer s.proxyRefreshMu.Unlock()
	s.proxyMu.RLock()
	cached, valid = s.proxyTiers[proxyGroupID]
	s.proxyMu.RUnlock()
	if valid && len(cached.ids) > 0 && time.Now().Before(cached.expiresAt) {
		return chooseHeartbeatProxy(cached.ids)
	}
	proxies := make([]Proxy, 0)
	for page := 1; ; page++ {
		filters := ProxyListFilters{Status: StatusActive}
		if proxyGroupID > 0 {
			groupID := proxyGroupID
			filters.ProxyGroupID = &groupID
		} else {
			filters.Ungrouped = true
		}
		batch, total, err := s.admin.ListProxies(ctx, page, 500, filters, "id", "asc")
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
	if len(proxies) > cfg.ProxyProbeSampleSize {
		sampled, err := sampleHeartbeatProxies(proxies, cfg.ProxyProbeSampleSize)
		if err != nil {
			return 0, err
		}
		proxies = sampled
	}
	type probeResult struct{ id, latency int64 }
	input := make(chan Proxy)
	output := make(chan probeResult, len(proxies))
	var wg sync.WaitGroup
	for i := 0; i < cfg.ProxyProbeWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for proxy := range input {
				probeCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.ProxyProbeTimeoutS)*time.Second)
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
	s.proxyTiers[proxyGroupID] = heartbeatProxyTier{ids: append([]int64(nil), tier...), expiresAt: time.Now().Add(time.Duration(cfg.ProxySweepTTLSecond) * time.Second)}
	s.proxyMu.Unlock()
	return chooseHeartbeatProxy(tier)
}

func heartbeatAccountCredentials(providerID string, credential *heartbeatVaultCredential) map[string]any {
	result := make(map[string]any)
	if credential != nil {
		for _, key := range []string{"base_url", "api_protocol", "account_mode", "tokenrhythm_cookie", "tr_session", "tr_csrf", "user_agent", "header_overrides"} {
			if value, ok := credential.Credentials[key]; ok && value != nil {
				result[key] = value
			}
		}
		result["api_key"] = credential.Key
	}
	if providerID == PlatformTokenRhythm {
		if _, exists := result["base_url"]; !exists {
			result["base_url"] = TokenRhythmDefaultBaseURL
		}
	}
	result["pool_mode"] = true
	result["pool_mode_retry_count"] = 3
	result["pool_mode_retry_status_codes"] = []int{401, 403, 429}
	return result
}

func (s *HeartbeatProvisioningService) fetchVaultCredential(ctx context.Context, sessionKey, fingerprint, providerID string) (*heartbeatVaultCredential, error) {
	provider, ok := normalizeHeartbeatProvider(providerID)
	if !ok {
		return nil, errors.New("unsupported heartbeat provider")
	}
	s.cfgMu.RLock()
	if s.vaultURL == nil {
		s.cfgMu.RUnlock()
		return nil, errors.New("heartbeat vault is not configured")
	}
	u := *s.vaultURL
	s.cfgMu.RUnlock()
	query := u.Query()
	query.Set("session_key", sessionKey)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("create heartbeat vault request")
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, errors.New("heartbeat vault request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("heartbeat vault returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, heartbeatMaxBodyBytes+1))
	if err != nil || len(body) > heartbeatMaxBodyBytes {
		return nil, errors.New("invalid heartbeat vault response")
	}
	var payload struct {
		OK   bool `json:"ok"`
		Keys []struct {
			Key         string         `json:"key"`
			Provider    string         `json:"provider"`
			Credentials map[string]any `json:"credentials"`
			BaseURL     string         `json:"base_url"`
			Session     string         `json:"tr_session"`
			CSRF        string         `json:"tr_csrf"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || !payload.OK {
		return nil, errors.New("invalid heartbeat vault payload")
	}
	for _, candidate := range payload.Keys {
		candidateProvider, candidateOK := normalizeHeartbeatProvider(candidate.Provider)
		if candidateOK && candidateProvider.ID == provider.ID && heartbeatFingerprint(candidate.Key) == jobFingerprint(fingerprint) {
			credentials := make(map[string]any, len(candidate.Credentials)+3)
			for key, value := range candidate.Credentials {
				credentials[key] = value
			}
			if strings.TrimSpace(candidate.BaseURL) != "" {
				credentials["base_url"] = strings.TrimSpace(candidate.BaseURL)
			}
			if strings.TrimSpace(candidate.Session) != "" {
				credentials["tr_session"] = strings.TrimSpace(candidate.Session)
			}
			if strings.TrimSpace(candidate.CSRF) != "" {
				credentials["tr_csrf"] = strings.TrimSpace(candidate.CSRF)
			}
			return &heartbeatVaultCredential{Key: strings.TrimSpace(candidate.Key), Credentials: credentials}, nil
		}
	}
	return nil, fmt.Errorf("matching %s key is not present in heartbeat vault", provider.ID)
}

func (s *HeartbeatProvisioningService) Options(ctx context.Context) (*HeartbeatProvisioningOptions, error) {
	if s == nil || s.admin == nil {
		return nil, errors.New("heartbeat admin service is unavailable")
	}
	groups, err := s.admin.GetAllGroups(ctx)
	if err != nil {
		return nil, err
	}
	result := &HeartbeatProvisioningOptions{
		Groups:      make([]HeartbeatGroupOption, 0, len(groups)),
		ProxyGroups: make([]HeartbeatProxyGroupOption, 0),
	}
	for _, group := range groups {
		if !isHeartbeatTargetGroupPlatform(group.Platform) {
			continue
		}
		result.Groups = append(result.Groups, HeartbeatGroupOption{ID: group.ID, Name: group.Name, Platform: group.Platform, Status: group.Status})
	}
	proxyGroups := make(map[int64]*HeartbeatProxyGroupOption)
	for page := 1; ; page++ {
		proxies, total, err := s.admin.ListProxies(ctx, page, 500, ProxyListFilters{Status: StatusActive}, "id", "asc")
		if err != nil {
			return nil, err
		}
		for _, proxy := range proxies {
			if proxy.ProxyGroupID == nil {
				option := proxyGroups[0]
				if option == nil {
					option = &HeartbeatProxyGroupOption{ID: 0, Name: "Unassigned proxies"}
					proxyGroups[0] = option
				}
				option.ActiveProxyCount++
				continue
			}
			option := proxyGroups[*proxy.ProxyGroupID]
			if option == nil {
				option = &HeartbeatProxyGroupOption{ID: *proxy.ProxyGroupID, Name: proxy.ProxyGroupName}
				proxyGroups[*proxy.ProxyGroupID] = option
			}
			option.ActiveProxyCount++
		}
		if len(proxies) == 0 || int64(page*500) >= total {
			break
		}
	}
	for _, group := range proxyGroups {
		result.ProxyGroups = append(result.ProxyGroups, *group)
	}
	sort.Slice(result.ProxyGroups, func(i, j int) bool { return result.ProxyGroups[i].ID < result.ProxyGroups[j].ID })
	return result, nil
}

func (s *HeartbeatProvisioningService) Status(ctx context.Context) (*HeartbeatProvisioningStatus, error) {
	if s == nil {
		return nil, errors.New("nil heartbeat provisioning service")
	}
	cfg, source := s.ConfigSnapshot()
	stats := &HeartbeatQueueStats{}
	if s.repo != nil {
		loaded, err := s.repo.Stats(ctx)
		if err != nil {
			return nil, err
		}
		if loaded != nil {
			stats = loaded
		}
	}
	var lastHeartbeat *time.Time
	if ns := s.lastHeartbeatNS.Load(); ns > 0 {
		value := time.Unix(0, ns).UTC()
		lastHeartbeat = &value
	}
	return &HeartbeatProvisioningStatus{
		Enabled:         cfg.Enabled,
		Running:         s.Running(),
		ConfigSource:    source,
		LastHeartbeatAt: lastHeartbeat,
		Queued:          stats.Queued,
		Processing:      stats.Processing,
		Retry:           stats.Retry,
		Failed:          stats.Failed,
		Complete:        stats.Complete,
		LastError:       stats.LastError,
		LastErrorAt:     stats.LastErrorAt,
	}, nil
}

func (s *HeartbeatProvisioningService) prepareConfig(ctx context.Context, requested config.HeartbeatProvisioningConfig) (*heartbeatPreparedConfig, error) {
	cfg := normalizeHeartbeatConfig(requested)
	if cfg.Enabled {
		if err := validateHeartbeatRuntimeConfig(cfg); err != nil {
			return nil, err
		}
		if s.admin == nil {
			return nil, errors.New("heartbeat admin service is unavailable")
		}
	}
	prepared := &heartbeatPreparedConfig{allowed: make(map[netip.Addr]struct{}, len(cfg.AllowedSourceIPs))}
	for _, rawIP := range cfg.AllowedSourceIPs {
		addr, err := netip.ParseAddr(strings.TrimSpace(rawIP))
		if err != nil {
			return nil, fmt.Errorf("heartbeat source IP is invalid: %w", err)
		}
		prepared.allowed[addr.Unmap()] = struct{}{}
	}
	if !cfg.Enabled {
		return prepared, nil
	}
	parsedURL, err := url.Parse(strings.TrimSpace(cfg.VaultURL))
	if err != nil || parsedURL == nil || parsedURL.Host == "" {
		return nil, fmt.Errorf("parse heartbeat vault URL: %w", err)
	}
	if parsedURL.Scheme != "https" && (parsedURL.Scheme != "http" || !cfg.AllowInsecureVault) {
		return nil, errors.New("heartbeat vault must use HTTPS unless insecure vault access is explicitly enabled")
	}
	prepared.vaultURL = parsedURL
	preflightCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, target := range cfg.Targets {
		group, err := s.admin.GetGroup(preflightCtx, target.GroupID)
		if err != nil {
			return nil, fmt.Errorf("load heartbeat target group %d: %w", target.GroupID, err)
		}
		if group == nil || group.Status != StatusActive {
			return nil, fmt.Errorf("heartbeat target group %d must be active", target.GroupID)
		}
		if !isHeartbeatTargetGroupPlatform(group.Platform) {
			return nil, fmt.Errorf("heartbeat target group %d uses unsupported platform %q", target.GroupID, group.Platform)
		}
		proxyFilters := ProxyListFilters{Status: StatusActive}
		if target.ProxyGroupID > 0 {
			proxyGroupID := target.ProxyGroupID
			proxyFilters.ProxyGroupID = &proxyGroupID
		} else {
			proxyFilters.Ungrouped = true
		}
		_, proxyTotal, err := s.admin.ListProxies(preflightCtx, 1, 1, proxyFilters, "id", "asc")
		if err != nil {
			return nil, fmt.Errorf("load heartbeat proxy pool %d: %w", target.ProxyGroupID, err)
		}
		if proxyTotal == 0 {
			return nil, fmt.Errorf("heartbeat proxy pool %d has no active proxies", target.ProxyGroupID)
		}
	}
	return prepared, nil
}

func (s *HeartbeatProvisioningService) configSnapshot() config.HeartbeatProvisioningConfig {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return cloneHeartbeatConfig(s.cfg)
}

func (s *HeartbeatProvisioningService) runtimeSnapshot() (config.HeartbeatProvisioningConfig, map[netip.Addr]struct{}, bool) {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return cloneHeartbeatConfig(s.cfg), s.allowed, s.cfg.Enabled
}

func (s *HeartbeatProvisioningService) enabledSnapshot() bool {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.Enabled
}

func (s *HeartbeatProvisioningService) clearProxyCache() {
	s.proxyMu.Lock()
	s.proxyTiers = make(map[int64]heartbeatProxyTier)
	s.proxyMu.Unlock()
}

func resolveHeartbeatTarget(cfg config.HeartbeatProvisioningConfig, groupID *int64) (config.HeartbeatProvisioningTarget, bool) {
	id := cfg.DefaultGroupID
	if groupID != nil {
		if *groupID <= 0 {
			return config.HeartbeatProvisioningTarget{}, false
		}
		id = *groupID
	}
	for _, target := range cfg.Targets {
		if target.GroupID == id {
			return target, true
		}
	}
	return config.HeartbeatProvisioningTarget{}, false
}

func resolveHeartbeatJobTarget(cfg config.HeartbeatProvisioningConfig, job *HeartbeatProvisioningJob) (config.HeartbeatProvisioningTarget, bool) {
	if job.TargetGroupID > 0 {
		if job.TargetProxyGroupID > 0 {
			return config.HeartbeatProvisioningTarget{GroupID: job.TargetGroupID, ProxyGroupID: job.TargetProxyGroupID}, true
		}
		// A zero proxy group is a valid ungrouped-pool target, but older jobs
		// may have omitted the target fields entirely. Resolve through current
		// configuration so those jobs still use the configured proxy policy.
		if target, ok := resolveHeartbeatTarget(cfg, &job.TargetGroupID); ok {
			return target, true
		}
	}
	return resolveHeartbeatTarget(cfg, nil)
}

func normalizeHeartbeatConfig(input config.HeartbeatProvisioningConfig) config.HeartbeatProvisioningConfig {
	output := cloneHeartbeatConfig(input)
	output.VaultURL = strings.TrimSpace(output.VaultURL)
	output.AllowedSourceIPs = normalizeHeartbeatIPs(output.AllowedSourceIPs)
	if output.DefaultGroupID <= 0 {
		output.DefaultGroupID = output.DeepSeekGroupID
	}
	if len(output.Targets) == 0 && output.DefaultGroupID > 0 && output.ProxyGroupID >= 0 {
		output.Targets = []config.HeartbeatProvisioningTarget{{GroupID: output.DefaultGroupID, ProxyGroupID: output.ProxyGroupID}}
	}
	if output.DefaultGroupID <= 0 && len(output.Targets) > 0 {
		output.DefaultGroupID = output.Targets[0].GroupID
	}
	if output.DeepSeekGroupID <= 0 {
		output.DeepSeekGroupID = output.DefaultGroupID
	}
	if output.ProxyGroupID <= 0 && len(output.Targets) == 1 {
		output.ProxyGroupID = output.Targets[0].ProxyGroupID
	}
	return output
}

func normalizeHeartbeatIPs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func cloneHeartbeatConfig(input config.HeartbeatProvisioningConfig) config.HeartbeatProvisioningConfig {
	output := input
	output.AllowedSourceIPs = append([]string(nil), input.AllowedSourceIPs...)
	output.Targets = append([]config.HeartbeatProvisioningTarget(nil), input.Targets...)
	return output
}

func validateHeartbeatRuntimeConfig(cfg config.HeartbeatProvisioningConfig) error {
	if cfg.DefaultGroupID <= 0 || cfg.WorkerCount < 1 || cfg.WorkerCount > 64 || cfg.ProxyProbeWorkers < 1 || cfg.ProxyProbeWorkers > 128 || cfg.ProxyProbeSampleSize < 1 || cfg.ProxyProbeSampleSize > 10000 || cfg.ProxyProbeTimeoutS < 1 || cfg.ProxyProbeTimeoutS > 60 || cfg.ProxySweepTTLSecond < 1 || cfg.ProxySweepTTLSecond > 86400 || cfg.MaxAttempts < 1 || cfg.MaxAttempts > 20 {
		return errors.New("heartbeat numeric settings are outside the allowed range")
	}
	if len(cfg.AllowedSourceIPs) == 0 {
		return errors.New("heartbeat allowed_source_ips is required")
	}
	seenGroups := make(map[int64]struct{}, len(cfg.Targets))
	hasDefault := false
	for _, target := range cfg.Targets {
		if target.GroupID <= 0 || target.ProxyGroupID < 0 {
			return errors.New("heartbeat targets must contain a positive group_id and a non-negative proxy_group_id")
		}
		if _, exists := seenGroups[target.GroupID]; exists {
			return errors.New("heartbeat targets contain duplicate group_id")
		}
		seenGroups[target.GroupID] = struct{}{}
		if target.GroupID == cfg.DefaultGroupID {
			hasDefault = true
		}
	}
	if len(cfg.Targets) == 0 || !hasDefault {
		return errors.New("heartbeat default_group_id must be present in targets")
	}
	if cfg.Enabled && strings.TrimSpace(cfg.VaultURL) == "" {
		return errors.New("heartbeat vault_url is required when enabled")
	}
	return nil
}

type heartbeatStoredConfig struct {
	Enabled              bool                                 `json:"enabled"`
	VaultURL             string                               `json:"vault_url"`
	AllowInsecureVault   bool                                 `json:"allow_insecure_vault"`
	AllowedSourceIPs     []string                             `json:"allowed_source_ips"`
	DefaultGroupID       int64                                `json:"default_group_id"`
	Targets              []config.HeartbeatProvisioningTarget `json:"targets"`
	WorkerCount          int                                  `json:"worker_count"`
	ProxyProbeWorkers    int                                  `json:"proxy_probe_workers"`
	ProxyProbeSampleSize int                                  `json:"proxy_probe_sample_size"`
	ProxyProbeTimeoutS   int                                  `json:"proxy_probe_timeout_seconds"`
	ProxySweepTTLSecond  int                                  `json:"proxy_sweep_ttl_seconds"`
	MaxAttempts          int                                  `json:"max_attempts"`
}

func encodeStoredHeartbeatConfig(cfg config.HeartbeatProvisioningConfig) (string, error) {
	stored := heartbeatStoredConfig{
		Enabled:              cfg.Enabled,
		VaultURL:             cfg.VaultURL,
		AllowInsecureVault:   cfg.AllowInsecureVault,
		AllowedSourceIPs:     append([]string(nil), cfg.AllowedSourceIPs...),
		DefaultGroupID:       cfg.DefaultGroupID,
		Targets:              append([]config.HeartbeatProvisioningTarget(nil), cfg.Targets...),
		WorkerCount:          cfg.WorkerCount,
		ProxyProbeWorkers:    cfg.ProxyProbeWorkers,
		ProxyProbeSampleSize: cfg.ProxyProbeSampleSize,
		ProxyProbeTimeoutS:   cfg.ProxyProbeTimeoutS,
		ProxySweepTTLSecond:  cfg.ProxySweepTTLSecond,
		MaxAttempts:          cfg.MaxAttempts,
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return "", fmt.Errorf("encode heartbeat configuration: %w", err)
	}
	return string(encoded), nil
}

func decodeStoredHeartbeatConfig(raw string) (config.HeartbeatProvisioningConfig, error) {
	var stored heartbeatStoredConfig
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return config.HeartbeatProvisioningConfig{}, err
	}
	return normalizeHeartbeatConfig(config.HeartbeatProvisioningConfig{
		Enabled:              stored.Enabled,
		VaultURL:             stored.VaultURL,
		AllowInsecureVault:   stored.AllowInsecureVault,
		AllowedSourceIPs:     stored.AllowedSourceIPs,
		DefaultGroupID:       stored.DefaultGroupID,
		Targets:              stored.Targets,
		WorkerCount:          stored.WorkerCount,
		ProxyProbeWorkers:    stored.ProxyProbeWorkers,
		ProxyProbeSampleSize: stored.ProxyProbeSampleSize,
		ProxyProbeTimeoutS:   stored.ProxyProbeTimeoutS,
		ProxySweepTTLSecond:  stored.ProxySweepTTLSecond,
		MaxAttempts:          stored.MaxAttempts,
	}), nil
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
