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

	if _, err := limiter.Wait(context.Background(), auth, auth.Provider, false); err != nil {
		t.Fatalf("first wait error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := limiter.Wait(ctx, auth, auth.Provider, false)
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
	limiter.OnResult(auth, auth.Provider, Result{
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
	_, err := limiter.Wait(ctx, auth, auth.Provider, false)
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

	release, err := limiter.Wait(context.Background(), auth, auth.Provider, true)
	if err != nil {
		t.Fatalf("first stream wait error: %v", err)
	}
	if release == nil {
		t.Fatal("first stream wait should return release callback")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err = limiter.Wait(ctx, auth, auth.Provider, true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second stream wait error = %v, want deadline exceeded", err)
	}

	release()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel2()
	release2, err := limiter.Wait(ctx2, auth, auth.Provider, true)
	if err != nil {
		t.Fatalf("third stream wait error: %v", err)
	}
	if release2 == nil {
		t.Fatal("third stream wait should return release callback")
	}
	release2()
}
