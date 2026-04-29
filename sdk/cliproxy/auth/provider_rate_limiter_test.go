package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestProviderRateLimiter_ProactiveWindow(t *testing.T) {
	enabled := true
	limiter := newProviderRateLimiter(&internalconfig.Config{
		ProviderRateLimit: internalconfig.ProviderRateLimitConfig{
			Enabled:                 &enabled,
			Scope:                   internalconfig.ProviderRateLimitScopeCredential,
			RateLimit:               1,
			RateWindowSeconds:       1,
			MaxStreamConcurrency:    5,
			ReactiveBaseDelayMS:     100,
			ReactiveMaxDelaySeconds: 1,
			ReactiveJitterMS:        0,
		},
	})
	auth := &Auth{ID: "auth-1", Provider: "openai-compatibility"}

	if _, err := limiter.Wait(context.Background(), auth, auth.Provider, "gpt-4.1", false); err != nil {
		t.Fatalf("first wait error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := limiter.Wait(ctx, auth, auth.Provider, "gpt-4.1", false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second wait error = %v, want deadline exceeded", err)
	}
}

func TestProviderRateLimiter_ReactiveBlock(t *testing.T) {
	enabled := true
	limiter := newProviderRateLimiter(&internalconfig.Config{
		ProviderRateLimit: internalconfig.ProviderRateLimitConfig{
			Enabled:                 &enabled,
			Scope:                   internalconfig.ProviderRateLimitScopeCredential,
			RateLimit:               100,
			RateWindowSeconds:       1,
			MaxStreamConcurrency:    5,
			ReactiveBaseDelayMS:     100,
			ReactiveMaxDelaySeconds: 1,
			ReactiveJitterMS:        0,
		},
	})
	auth := &Auth{ID: "auth-2", Provider: "openai-compatibility"}
	retryAfter := 120 * time.Millisecond
	limiter.OnResult(auth, auth.Provider, "gpt-4.1", Result{
		AuthID:     auth.ID,
		Provider:   auth.Provider,
		Success:    false,
		RetryAfter: &retryAfter,
		Error: &Error{
			HTTPStatus: 429,
			Message:    "rate limited",
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := limiter.Wait(ctx, auth, auth.Provider, "gpt-4.1", false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait error = %v, want deadline exceeded", err)
	}
}

func TestProviderRateLimiter_StreamConcurrency(t *testing.T) {
	enabled := true
	limiter := newProviderRateLimiter(&internalconfig.Config{
		ProviderRateLimit: internalconfig.ProviderRateLimitConfig{
			Enabled:                 &enabled,
			Scope:                   internalconfig.ProviderRateLimitScopeCredential,
			RateLimit:               100,
			RateWindowSeconds:       1,
			MaxStreamConcurrency:    1,
			ReactiveBaseDelayMS:     100,
			ReactiveMaxDelaySeconds: 1,
			ReactiveJitterMS:        0,
		},
	})
	auth := &Auth{ID: "auth-3", Provider: "openai-compatibility"}

	release, err := limiter.Wait(context.Background(), auth, auth.Provider, "gpt-4.1", true)
	if err != nil {
		t.Fatalf("first stream wait error: %v", err)
	}
	if release == nil {
		t.Fatal("first stream wait should return release callback")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err = limiter.Wait(ctx, auth, auth.Provider, "gpt-4.1", true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second stream wait error = %v, want deadline exceeded", err)
	}

	release()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel2()
	release2, err := limiter.Wait(ctx2, auth, auth.Provider, "gpt-4.1", true)
	if err != nil {
		t.Fatalf("third stream wait error: %v", err)
	}
	if release2 == nil {
		t.Fatal("third stream wait should return release callback")
	}
	release2()
}

func TestProviderRateLimiter_ProviderModelIsolation(t *testing.T) {
	enabled := true
	limiter := newProviderRateLimiter(&internalconfig.Config{
		ProviderRateLimit: internalconfig.ProviderRateLimitConfig{
			Enabled:                 &enabled,
			Scope:                   internalconfig.ProviderRateLimitScopeProviderModel,
			RateLimit:               1,
			RateWindowSeconds:       1,
			MaxStreamConcurrency:    1,
			ReactiveBaseDelayMS:     100,
			ReactiveMaxDelaySeconds: 1,
			ReactiveJitterMS:        0,
		},
	})
	auth := &Auth{ID: "auth-4", Provider: "openai-compatibility"}

	if _, err := limiter.Wait(context.Background(), auth, auth.Provider, "model-a", false); err != nil {
		t.Fatalf("first wait error: %v", err)
	}
	if _, err := limiter.Wait(context.Background(), auth, auth.Provider, "model-b", false); err != nil {
		t.Fatalf("second model should not be throttled: %v", err)
	}
}

func TestProviderRateLimiter_AdaptiveDecreasePersistsAutoOverride(t *testing.T) {
	enabled := true
	adaptiveEnabled := true
	adaptiveIncrease := false
	cfg := internalconfig.ProviderRateLimitConfig{
		Enabled:                   &enabled,
		Scope:                     internalconfig.ProviderRateLimitScopeProviderModel,
		RateLimit:                 40,
		RateWindowSeconds:         60,
		MaxStreamConcurrency:      5,
		ReactiveBaseDelayMS:       100,
		ReactiveMaxDelaySeconds:   1,
		ReactiveJitterMS:          0,
		AdaptiveEnabled:           &adaptiveEnabled,
		AdaptiveIncreaseOnSuccess: &adaptiveIncrease,
		AdaptiveDecreaseFactor:    0.5,
		AdaptiveMinRateLimit:      1,
	}
	limiter := newProviderRateLimiter(&internalconfig.Config{
		ProviderRateLimit: cfg,
	})
	updated := cfg
	limiter.SetConfigMutator(func(apply func(*internalconfig.ProviderRateLimitConfig) bool) {
		if apply(&updated) {
			return
		}
	})
	auth := &Auth{ID: "auth-5", Provider: "openai-compatibility"}
	limiter.OnResult(auth, auth.Provider, "gpt-4.1", Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-4.1",
		Success:  false,
		Error: &Error{
			HTTPStatus: 429,
			Message:    "rate limited",
		},
	})

	if len(updated.Overrides) != 1 {
		t.Fatalf("overrides len = %d, want 1", len(updated.Overrides))
	}
	got := updated.Overrides[0]
	if got.Provider != "openai-compatibility" {
		t.Fatalf("override provider = %q", got.Provider)
	}
	if got.Model != "gpt-4.1" {
		t.Fatalf("override model = %q", got.Model)
	}
	if got.Mode != internalconfig.ProviderRateLimitModeAuto {
		t.Fatalf("override mode = %q", got.Mode)
	}
	if got.RateLimit != 20 {
		t.Fatalf("override rate-limit = %d, want 20", got.RateLimit)
	}
}

func TestProviderRateLimiter_AdaptiveIncreaseOnSuccess(t *testing.T) {
	enabled := true
	adaptiveEnabled := true
	adaptiveIncrease := true
	cfg := internalconfig.ProviderRateLimitConfig{
		Enabled:                   &enabled,
		Scope:                     internalconfig.ProviderRateLimitScopeProviderModel,
		RateLimit:                 40,
		RateWindowSeconds:         60,
		MaxStreamConcurrency:      5,
		ReactiveBaseDelayMS:       100,
		ReactiveMaxDelaySeconds:   1,
		ReactiveJitterMS:          0,
		AdaptiveEnabled:           &adaptiveEnabled,
		AdaptiveIncreaseOnSuccess: &adaptiveIncrease,
		AdaptiveDecreaseFactor:    0.5,
		AdaptiveMinRateLimit:      1,
		Overrides: []internalconfig.ProviderRateLimitOverride{
			{
				Provider:  "openai-compatibility",
				AuthID:    "auth-6",
				Model:     "gpt-4.1",
				Mode:      internalconfig.ProviderRateLimitModeAuto,
				Scope:     internalconfig.ProviderRateLimitScopeProviderModel,
				RateLimit: 10,
			},
		},
	}
	limiter := newProviderRateLimiter(&internalconfig.Config{
		ProviderRateLimit: cfg,
	})
	updated := cfg
	limiter.SetConfigMutator(func(apply func(*internalconfig.ProviderRateLimitConfig) bool) {
		_ = apply(&updated)
	})
	auth := &Auth{ID: "auth-6", Provider: "openai-compatibility"}
	limiter.OnResult(auth, auth.Provider, "gpt-4.1", Result{
		AuthID:   auth.ID,
		Provider: auth.Provider,
		Model:    "gpt-4.1",
		Success:  true,
	})

	if len(updated.Overrides) != 1 {
		t.Fatalf("overrides len = %d, want 1", len(updated.Overrides))
	}
	got := updated.Overrides[0]
	if got.RateLimit != 11 {
		t.Fatalf("override rate-limit = %d, want 11", got.RateLimit)
	}
	if got.Mode != internalconfig.ProviderRateLimitModeAuto {
		t.Fatalf("override mode = %q, want %q", got.Mode, internalconfig.ProviderRateLimitModeAuto)
	}
}
