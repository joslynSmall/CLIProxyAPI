package auth

import (
	"context"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
)

const providerRateLimiterStreamPollInterval = 50 * time.Millisecond

type providerRateLimiterPolicy struct {
	enabled                   bool
	scope                     string
	rateLimit                 int
	adaptiveMaxRateLimit      int
	window                    time.Duration
	maxStreamConcurrency      int
	reactiveBaseDelay         time.Duration
	reactiveMaxDelay          time.Duration
	reactiveJitter            time.Duration
	adaptiveEnabled           bool
	adaptiveIncreaseOnSuccess bool
	adaptiveDecreaseFactor    float64
	adaptiveMinRateLimit      int
	adaptiveMode              string
}

type providerRateLimiterBucket struct {
	requests       []time.Time
	blockedUntil   time.Time
	backoffAttempt int
	activeStreams  int
}

type providerRateLimiter struct {
	mu            sync.Mutex
	config        internalconfig.ProviderRateLimitConfig
	buckets       map[string]*providerRateLimiterBucket
	configMutator func(func(*internalconfig.ProviderRateLimitConfig) bool)
}

func newProviderRateLimiter(cfg *internalconfig.Config) *providerRateLimiter {
	limiter := &providerRateLimiter{
		buckets: make(map[string]*providerRateLimiterBucket),
	}
	limiter.UpdateConfig(cfg)
	return limiter
}

func (l *providerRateLimiter) UpdateConfig(cfg *internalconfig.Config) {
	if l == nil {
		return
	}
	next := internalconfig.DefaultProviderRateLimitConfig()
	if cfg != nil {
		if normalized, err := internalconfig.NormalizeProviderRateLimitConfig(cfg.ProviderRateLimit); err == nil {
			next = normalized
		}
	}
	l.mu.Lock()
	l.config = next
	l.mu.Unlock()
}

func (l *providerRateLimiter) SetConfigMutator(mutator func(func(*internalconfig.ProviderRateLimitConfig) bool)) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.configMutator = mutator
	l.mu.Unlock()
}

func (l *providerRateLimiter) Wait(ctx context.Context, auth *Auth, provider, model string, stream bool) (func(), error) {
	if l == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		wait, release, done := l.tryAcquire(auth, provider, model, stream)
		if done {
			return release, nil
		}
		if wait <= 0 {
			wait = 10 * time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *providerRateLimiter) tryAcquire(auth *Auth, provider, model string, stream bool) (time.Duration, func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	providerKey := providerRateLimitProviderKey(auth, provider)
	modelKey := providerRateLimitModelKey(model)
	policy := l.resolvePolicyLocked(auth, providerKey, modelKey)
	if !policy.enabled {
		return 0, nil, true
	}
	key, ok := l.scopeKeyLocked(policy, auth, providerKey, modelKey)
	if !ok {
		return 0, nil, true
	}
	bucket := l.bucketLocked(key)
	now := time.Now()
	bucket.prune(now.Add(-policy.window))

	if bucket.blockedUntil.After(now) {
		return bucket.blockedUntil.Sub(now), nil, false
	}

	if policy.rateLimit > 0 && len(bucket.requests) >= policy.rateLimit {
		readyAt := bucket.requests[0].Add(policy.window)
		wait := readyAt.Sub(now)
		if wait > 0 {
			return wait, nil, false
		}
	}

	if stream && policy.maxStreamConcurrency > 0 && bucket.activeStreams >= policy.maxStreamConcurrency {
		return providerRateLimiterStreamPollInterval, nil, false
	}

	bucket.requests = append(bucket.requests, now)
	if stream {
		bucket.activeStreams++
		var releaseOnce sync.Once
		return 0, func() {
			releaseOnce.Do(func() {
				l.releaseStream(key)
			})
		}, true
	}
	return 0, nil, true
}

func (l *providerRateLimiter) OnResult(auth *Auth, provider, model string, result Result) {
	if l == nil {
		return
	}
	providerKey := providerRateLimitProviderKey(auth, provider)
	modelKey := providerRateLimitModelKey(model)
	var adaptiveUpdate *providerRateLimitAdaptiveUpdate
	l.mu.Lock()
	policy := l.resolvePolicyLocked(auth, providerKey, modelKey)
	if !policy.enabled {
		l.mu.Unlock()
		return
	}
	key, ok := l.scopeKeyLocked(policy, auth, providerKey, modelKey)
	if !ok {
		l.mu.Unlock()
		return
	}
	bucket := l.bucketLocked(key)
	now := time.Now()
	if result.Success {
		if !bucket.blockedUntil.After(now) {
			bucket.backoffAttempt = 0
		}
		if policy.adaptiveEnabled &&
			policy.adaptiveIncreaseOnSuccess &&
			policy.adaptiveMode != internalconfig.ProviderRateLimitModeManual {
			if increasedRate := adaptiveIncreasedRateLimit(policy.rateLimit, policy.adaptiveMaxRateLimit); increasedRate > policy.rateLimit {
				adaptiveUpdate = &providerRateLimitAdaptiveUpdate{
					provider: providerKey,
					authID:   providerRateLimitAuthID(auth),
					model:    modelKey,
					scope:    policy.scope,
					rate:     increasedRate,
				}
			}
		}
		l.mu.Unlock()
		l.persistAdaptiveUpdate(adaptiveUpdate)
		return
	}
	status := statusCodeFromResult(result.Error)
	if status != 429 {
		l.mu.Unlock()
		return
	}
	delay := l.reactiveDelayLocked(bucket, policy, result.RetryAfter)
	if delay <= 0 {
		l.mu.Unlock()
		return
	}
	next := now.Add(delay)
	if next.After(bucket.blockedUntil) {
		bucket.blockedUntil = next
	}
	if policy.adaptiveEnabled && policy.adaptiveMode != internalconfig.ProviderRateLimitModeManual {
		if decreasedRate := adaptiveDecreasedRateLimit(policy.rateLimit, policy.adaptiveDecreaseFactor, policy.adaptiveMinRateLimit); decreasedRate > 0 && decreasedRate < policy.rateLimit {
			adaptiveUpdate = &providerRateLimitAdaptiveUpdate{
				provider: providerKey,
				authID:   providerRateLimitAuthID(auth),
				model:    modelKey,
				scope:    policy.scope,
				rate:     decreasedRate,
			}
		}
	}
	l.mu.Unlock()
	l.persistAdaptiveUpdate(adaptiveUpdate)
}

func (l *providerRateLimiter) reactiveDelayLocked(
	bucket *providerRateLimiterBucket,
	policy providerRateLimiterPolicy,
	retryAfter *time.Duration,
) time.Duration {
	if bucket == nil {
		return 0
	}
	if retryAfter != nil && *retryAfter > 0 {
		bucket.backoffAttempt = 0
		return *retryAfter
	}
	delay := policy.reactiveBaseDelay
	if delay <= 0 {
		delay = time.Second
	}
	maxDelay := policy.reactiveMaxDelay
	if maxDelay <= 0 {
		maxDelay = 60 * time.Second
	}
	if bucket.backoffAttempt > 0 {
		for step := 0; step < bucket.backoffAttempt; step++ {
			if delay >= maxDelay {
				delay = maxDelay
				break
			}
			next := delay * 2
			if next > maxDelay {
				delay = maxDelay
				break
			}
			delay = next
		}
	}
	if policy.reactiveJitter > 0 {
		jitter := time.Duration(rand.Int63n(policy.reactiveJitter.Nanoseconds() + 1))
		delay += jitter
	}
	if delay > maxDelay {
		delay = maxDelay
	}
	if bucket.backoffAttempt < 16 {
		bucket.backoffAttempt++
	}
	return delay
}

func (l *providerRateLimiter) resolvePolicyLocked(auth *Auth, providerKey, modelKey string) providerRateLimiterPolicy {
	cfg := l.config
	policy := providerRateLimiterPolicy{
		enabled:                   cfg.EnabledOrDefault(),
		scope:                     cfg.Scope,
		rateLimit:                 cfg.RateLimit,
		adaptiveMaxRateLimit:      cfg.RateLimit,
		window:                    time.Duration(cfg.RateWindowSeconds) * time.Second,
		maxStreamConcurrency:      cfg.MaxStreamConcurrency,
		reactiveBaseDelay:         time.Duration(cfg.ReactiveBaseDelayMS) * time.Millisecond,
		reactiveMaxDelay:          time.Duration(cfg.ReactiveMaxDelaySeconds) * time.Second,
		reactiveJitter:            time.Duration(cfg.ReactiveJitterMS) * time.Millisecond,
		adaptiveEnabled:           cfg.AdaptiveEnabled != nil && *cfg.AdaptiveEnabled,
		adaptiveIncreaseOnSuccess: cfg.AdaptiveIncreaseOnSuccess != nil && *cfg.AdaptiveIncreaseOnSuccess,
		adaptiveDecreaseFactor:    cfg.AdaptiveDecreaseFactor,
		adaptiveMinRateLimit:      cfg.AdaptiveMinRateLimit,
		adaptiveMode:              internalconfig.ProviderRateLimitModeAuto,
	}
	for _, override := range cfg.Overrides {
		if !providerRateLimitOverrideMatches(override, auth, providerKey, modelKey) {
			continue
		}
		if override.Enabled != nil {
			policy.enabled = *override.Enabled
		}
		if override.Scope != "" {
			policy.scope = override.Scope
		}
		if override.RateLimit > 0 {
			policy.rateLimit = override.RateLimit
		}
		if override.RateWindowSeconds > 0 {
			policy.window = time.Duration(override.RateWindowSeconds) * time.Second
		}
		if override.MaxStreamConcurrency > 0 {
			policy.maxStreamConcurrency = override.MaxStreamConcurrency
		}
		if override.ReactiveBaseDelayMS > 0 {
			policy.reactiveBaseDelay = time.Duration(override.ReactiveBaseDelayMS) * time.Millisecond
		}
		if override.ReactiveMaxDelaySeconds > 0 {
			policy.reactiveMaxDelay = time.Duration(override.ReactiveMaxDelaySeconds) * time.Second
		}
		if override.ReactiveJitterMS > 0 {
			policy.reactiveJitter = time.Duration(override.ReactiveJitterMS) * time.Millisecond
		}
		if override.Mode != "" {
			policy.adaptiveMode = override.Mode
		}
	}
	if policy.scope == "" {
		policy.scope = internalconfig.ProviderRateLimitScopeCredential
	}
	if policy.rateLimit <= 0 {
		policy.rateLimit = internalconfig.DefaultProviderRateLimit
	}
	if policy.adaptiveMaxRateLimit <= 0 {
		policy.adaptiveMaxRateLimit = internalconfig.DefaultProviderRateLimit
	}
	if policy.window <= 0 {
		policy.window = time.Duration(internalconfig.DefaultProviderRateWindowSec) * time.Second
	}
	if policy.maxStreamConcurrency <= 0 {
		policy.maxStreamConcurrency = internalconfig.DefaultProviderMaxConcurrent
	}
	if policy.reactiveBaseDelay <= 0 {
		policy.reactiveBaseDelay = time.Duration(internalconfig.DefaultProviderReactiveBase) * time.Millisecond
	}
	if policy.reactiveMaxDelay <= 0 {
		policy.reactiveMaxDelay = time.Duration(internalconfig.DefaultProviderReactiveMaxSec) * time.Second
	}
	if policy.reactiveJitter < 0 {
		policy.reactiveJitter = 0
	}
	if policy.adaptiveDecreaseFactor <= 0 || policy.adaptiveDecreaseFactor >= 1 {
		policy.adaptiveDecreaseFactor = internalconfig.DefaultProviderAdaptiveDecreaseFactor
	}
	if policy.adaptiveMinRateLimit <= 0 {
		policy.adaptiveMinRateLimit = internalconfig.DefaultProviderAdaptiveMinRateLimit
	}
	if policy.adaptiveMode == "" {
		policy.adaptiveMode = internalconfig.ProviderRateLimitModeAuto
	}
	return policy
}

func providerRateLimitOverrideMatches(
	override internalconfig.ProviderRateLimitOverride,
	auth *Auth,
	providerKey string,
	modelKey string,
) bool {
	if override.Provider != "" && !strings.EqualFold(override.Provider, providerKey) {
		return false
	}
	if override.AuthID != "" {
		if auth == nil || !strings.EqualFold(strings.TrimSpace(auth.ID), override.AuthID) {
			return false
		}
	}
	if model := providerRateLimitModelKey(override.Model); model != "" && !strings.EqualFold(model, modelKey) {
		return false
	}
	return override.Provider != "" || override.AuthID != ""
}

func providerRateLimitProviderKey(auth *Auth, provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "" {
		return provider
	}
	if auth == nil {
		return ""
	}
	if auth.Attributes != nil {
		if providerKey := strings.TrimSpace(auth.Attributes["provider_key"]); providerKey != "" {
			return strings.ToLower(providerKey)
		}
	}
	return strings.ToLower(strings.TrimSpace(auth.Provider))
}

func (l *providerRateLimiter) scopeKeyLocked(policy providerRateLimiterPolicy, auth *Auth, providerKey, modelKey string) (string, bool) {
	switch policy.scope {
	case internalconfig.ProviderRateLimitScopeProvider:
		if providerKey == "" {
			return "", false
		}
		return "provider:" + providerKey, true
	case internalconfig.ProviderRateLimitScopeProviderModel:
		if providerKey == "" || modelKey == "" {
			return "", false
		}
		return "provider:" + providerKey + "|model:" + modelKey, true
	default:
		credentialKey := ""
		if auth != nil {
			credentialKey = strings.TrimSpace(auth.ID)
			if credentialKey == "" {
				credentialKey = strings.TrimSpace(auth.EnsureIndex())
			}
		}
		if credentialKey == "" {
			if providerKey == "" {
				return "", false
			}
			return "provider:" + providerKey, true
		}
		return "credential:" + credentialKey, true
	}
}

func providerRateLimitModelKey(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	base := strings.TrimSpace(thinking.ParseSuffix(model).ModelName)
	if base != "" {
		model = base
	}
	return strings.ToLower(strings.TrimSpace(model))
}

func providerRateLimitAuthID(auth *Auth) string {
	if auth == nil {
		return ""
	}
	return strings.TrimSpace(auth.ID)
}

func adaptiveDecreasedRateLimit(current int, factor float64, minLimit int) int {
	if current <= 0 {
		return 0
	}
	if minLimit <= 0 {
		minLimit = 1
	}
	if factor <= 0 || factor >= 1 {
		factor = internalconfig.DefaultProviderAdaptiveDecreaseFactor
	}
	next := int(math.Floor(float64(current) * factor))
	if next >= current {
		next = current - 1
	}
	if next < minLimit {
		next = minLimit
	}
	return next
}

func adaptiveIncreasedRateLimit(current int, maxLimit int) int {
	if current <= 0 {
		return 0
	}
	if maxLimit <= 0 {
		maxLimit = internalconfig.DefaultProviderRateLimit
	}
	if current >= maxLimit {
		return current
	}
	return current + 1
}

type providerRateLimitAdaptiveUpdate struct {
	provider string
	authID   string
	model    string
	scope    string
	rate     int
}

func (l *providerRateLimiter) persistAdaptiveUpdate(update *providerRateLimitAdaptiveUpdate) {
	if l == nil || update == nil || update.rate <= 0 {
		return
	}
	l.mu.Lock()
	mutator := l.configMutator
	l.mu.Unlock()
	if mutator == nil {
		return
	}
	mutator(func(cfg *internalconfig.ProviderRateLimitConfig) bool {
		if cfg == nil {
			return false
		}
		if cfg.Overrides == nil {
			cfg.Overrides = make([]internalconfig.ProviderRateLimitOverride, 0, 1)
		}
		for i := range cfg.Overrides {
			override := &cfg.Overrides[i]
			if !providerRateLimitOverrideKeyMatches(*override, update.provider, update.authID, update.model) {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(override.Mode), internalconfig.ProviderRateLimitModeManual) {
				return false
			}
			changed := false
			if override.RateLimit != update.rate {
				override.RateLimit = update.rate
				changed = true
			}
			if strings.TrimSpace(override.Mode) != internalconfig.ProviderRateLimitModeAuto {
				override.Mode = internalconfig.ProviderRateLimitModeAuto
				changed = true
			}
			return changed
		}
		cfg.Overrides = append(cfg.Overrides, internalconfig.ProviderRateLimitOverride{
			Provider:  update.provider,
			AuthID:    update.authID,
			Model:     update.model,
			Mode:      internalconfig.ProviderRateLimitModeAuto,
			Scope:     update.scope,
			RateLimit: update.rate,
		})
		return true
	})
}

func providerRateLimitOverrideKeyMatches(
	override internalconfig.ProviderRateLimitOverride,
	provider string,
	authID string,
	model string,
) bool {
	if strings.TrimSpace(override.Provider) != strings.TrimSpace(provider) {
		return false
	}
	if strings.TrimSpace(override.AuthID) != strings.TrimSpace(authID) {
		return false
	}
	return providerRateLimitModelKey(override.Model) == providerRateLimitModelKey(model)
}

func (l *providerRateLimiter) bucketLocked(key string) *providerRateLimiterBucket {
	bucket := l.buckets[key]
	if bucket == nil {
		bucket = &providerRateLimiterBucket{}
		l.buckets[key] = bucket
	}
	return bucket
}

func (l *providerRateLimiter) releaseStream(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket := l.buckets[key]
	if bucket == nil {
		return
	}
	if bucket.activeStreams > 0 {
		bucket.activeStreams--
	}
}

func (b *providerRateLimiterBucket) prune(cutoff time.Time) {
	if b == nil || len(b.requests) == 0 {
		return
	}
	idx := 0
	for idx < len(b.requests) && b.requests[idx].Before(cutoff) {
		idx++
	}
	if idx == 0 {
		return
	}
	if idx >= len(b.requests) {
		b.requests = b.requests[:0]
		return
	}
	trimmed := make([]time.Time, len(b.requests)-idx)
	copy(trimmed, b.requests[idx:])
	b.requests = trimmed
}
