package auth

import (
	"net/http"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

func TestAuthProviderRoutingScope(t *testing.T) {
	t.Run("nil auth", func(t *testing.T) {
		p, pk := authProviderRoutingScope(nil)
		if p != "" || pk != "" {
			t.Fatalf("expected empty, got (%q, %q)", p, pk)
		}
	})

	t.Run("provider only", func(t *testing.T) {
		a := &Auth{Provider: "gemini"}
		p, pk := authProviderRoutingScope(a)
		if p != "gemini" || pk != "" {
			t.Fatalf("expected (gemini, ''), got (%q, %q)", p, pk)
		}
	})

	t.Run("provider + provider_key from attributes", func(t *testing.T) {
		a := &Auth{
			Provider: "openai-compatibility",
			Attributes: map[string]string{
				"provider_key": "deepseek",
			},
		}
		p, pk := authProviderRoutingScope(a)
		if p != "openai-compatibility" || pk != "deepseek" {
			t.Fatalf("expected (openai-compatibility, deepseek), got (%q, %q)", p, pk)
		}
	})

	t.Run("compat_name as provider fallback", func(t *testing.T) {
		a := &Auth{
			Provider: "",
			Attributes: map[string]string{
				"compat_name": "my-compat",
			},
		}
		p, pk := authProviderRoutingScope(a)
		if p != "my-compat" || pk != "" {
			t.Fatalf("expected (my-compat, ''), got (%q, %q)", p, pk)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		a := &Auth{
			Provider: "Gemini",
			Attributes: map[string]string{
				"provider_key": "DeepSeek",
			},
		}
		p, pk := authProviderRoutingScope(a)
		if p != "gemini" || pk != "deepseek" {
			t.Fatalf("expected lowercase, got (%q, %q)", p, pk)
		}
	})
}

func newTestManagerFor429Routing(cfg *internalconfig.Config) *Manager {
	m := &Manager{}
	if cfg != nil {
		m.runtimeConfig.Store(cfg)
	}
	return m
}

func TestSameModelFailoverEnabledForAuth(t *testing.T) {
	t.Run("nil auth returns true", func(t *testing.T) {
		m := newTestManagerFor429Routing(nil)
		if !m.sameModelFailoverEnabledForAuth(nil) {
			t.Fatal("expected true for nil auth")
		}
	})

	t.Run("auth attribute overrides all", func(t *testing.T) {
		m := newTestManagerFor429Routing(nil)
		a := &Auth{Attributes: map[string]string{"same_model_failover": "false"}}
		if m.sameModelFailoverEnabledForAuth(a) {
			t.Fatal("expected false from auth attribute")
		}
	})

	t.Run("runtime config override when no auth attribute", func(t *testing.T) {
		smf := false
	cfg := &internalconfig.Config{
		HTTP429Routing: internalconfig.NormalizeHTTP429RoutingConfig(internalconfig.HTTP429RoutingConfig{
			Overrides: []internalconfig.HTTP429RoutingOverride{
				{Provider: "gemini", SameModelFailover: &smf},
			},
		}),
	}
	m := newTestManagerFor429Routing(cfg)
	a := &Auth{Provider: "gemini"}
	if m.sameModelFailoverEnabledForAuth(a) {
		t.Fatal("expected false from runtime config override")
	}
})

	t.Run("runtime config global default when no override", func(t *testing.T) {
	cfg := &internalconfig.Config{
		HTTP429Routing: internalconfig.NormalizeHTTP429RoutingConfig(internalconfig.HTTP429RoutingConfig{}),
	}
	m := newTestManagerFor429Routing(cfg)
	a := &Auth{Provider: "claude"}
	if !m.sameModelFailoverEnabledForAuth(a) {
		t.Fatal("expected true from global default")
	}
})

	t.Run("auth attribute takes precedence over runtime config", func(t *testing.T) {
	smf := false
	cfg := &internalconfig.Config{
		HTTP429Routing: internalconfig.NormalizeHTTP429RoutingConfig(internalconfig.HTTP429RoutingConfig{
			Overrides: []internalconfig.HTTP429RoutingOverride{
				{Provider: "gemini", SameModelFailover: &smf},
			},
		}),
	}
		m := newTestManagerFor429Routing(cfg)
		a := &Auth{
			Provider:   "gemini",
			Attributes: map[string]string{"same_model_failover": "true"},
		}
		if !m.sameModelFailoverEnabledForAuth(a) {
			t.Fatal("auth attribute should take precedence over runtime config override")
		}
	})
}

func TestHTTP429RoutingPolicyForAuth(t *testing.T) {
	t.Run("nil auth returns immediate_failover", func(t *testing.T) {
		m := newTestManagerFor429Routing(nil)
		if got := m.http429RoutingPolicyForAuth(nil); got != internalconfig.HTTP429RoutingPolicyImmediateFailover {
			t.Fatalf("expected %q, got %q", internalconfig.HTTP429RoutingPolicyImmediateFailover, got)
		}
	})

	t.Run("auth attribute overrides all", func(t *testing.T) {
		m := newTestManagerFor429Routing(nil)
		a := &Auth{Attributes: map[string]string{"http_429_routing_policy": "failover_after_cooldown_window"}}
		if got := m.http429RoutingPolicyForAuth(a); got != internalconfig.HTTP429RoutingPolicyFailoverAfterCooldownWindow {
			t.Fatalf("expected %q from auth attribute, got %q", internalconfig.HTTP429RoutingPolicyFailoverAfterCooldownWindow, got)
		}
	})

	t.Run("legacy 429_routing_policy key also works", func(t *testing.T) {
		m := newTestManagerFor429Routing(nil)
		a := &Auth{Attributes: map[string]string{"429_routing_policy": "failover_after_cooldown_window"}}
		if got := m.http429RoutingPolicyForAuth(a); got != internalconfig.HTTP429RoutingPolicyFailoverAfterCooldownWindow {
			t.Fatalf("expected %q from legacy key, got %q", internalconfig.HTTP429RoutingPolicyFailoverAfterCooldownWindow, got)
		}
	})

	t.Run("runtime config override when no auth attribute", func(t *testing.T) {
		cfg := &internalconfig.Config{
			HTTP429Routing: internalconfig.NormalizeHTTP429RoutingConfig(internalconfig.HTTP429RoutingConfig{
				Overrides: []internalconfig.HTTP429RoutingOverride{
					{Provider: "gemini", RoutingPolicy: internalconfig.HTTP429RoutingPolicyFailoverAfterCooldownWindow},
				},
			}),
		}
		m := newTestManagerFor429Routing(cfg)
		a := &Auth{Provider: "gemini"}
		if got := m.http429RoutingPolicyForAuth(a); got != internalconfig.HTTP429RoutingPolicyFailoverAfterCooldownWindow {
			t.Fatalf("expected %q from runtime config override, got %q", internalconfig.HTTP429RoutingPolicyFailoverAfterCooldownWindow, got)
		}
	})

	t.Run("runtime config global default when no override", func(t *testing.T) {
		cfg := &internalconfig.Config{
			HTTP429Routing: internalconfig.NormalizeHTTP429RoutingConfig(internalconfig.HTTP429RoutingConfig{}),
		}
		m := newTestManagerFor429Routing(cfg)
		a := &Auth{Provider: "claude"}
		if got := m.http429RoutingPolicyForAuth(a); got != internalconfig.HTTP429RoutingPolicyImmediateFailover {
			t.Fatalf("expected %q from global default, got %q", internalconfig.HTTP429RoutingPolicyImmediateFailover, got)
		}
	})
}

func TestShouldPauseOnHTTP429_AllProviders(t *testing.T) {
	cfg := &internalconfig.Config{
		HTTP429Routing: internalconfig.NormalizeHTTP429RoutingConfig(internalconfig.HTTP429RoutingConfig{}),
	}
	m := newTestManagerFor429Routing(cfg)

	err429 := &retryAfterStatusError{status: http.StatusTooManyRequests, message: "rate limited"}

	t.Run("gemini 429 不再被跳过", func(t *testing.T) {
		a := &Auth{
			ID:        "gemini-auth",
			Provider:  "gemini",
			Attributes: map[string]string{
				"same_model_failover":    "true",
				"http_429_routing_policy": "immediate_failover",
			},
		}
		opts := cliproxyexecutor.Options{Metadata: map[string]any{"_init": true}}
		if m.shouldPauseOnHTTP429(a, opts, err429) {
			t.Fatal("gemini immediate_failover 不应 pause")
		}
	})

	t.Run("claude 429 with cooldown window 应 pause", func(t *testing.T) {
		a := &Auth{
			ID:        "claude-auth",
			Provider:  "claude",
			Attributes: map[string]string{
				"same_model_failover":    "true",
				"http_429_routing_policy": "failover_after_cooldown_window",
			},
		}
		opts := cliproxyexecutor.Options{Metadata: map[string]any{"_init": true}}
		if !m.shouldPauseOnHTTP429(a, opts, err429) {
			t.Fatal("claude cooldown window 应 pause")
		}
	})

	t.Run("codex 429 with same-model-failover=false 应 pause", func(t *testing.T) {
		a := &Auth{
			ID: "codex-auth",
			Provider: "codex",
			Attributes: map[string]string{
				"same_model_failover": "false",
			},
		}
		opts := cliproxyexecutor.Options{Metadata: map[string]any{"_init": true}}
		if !m.shouldPauseOnHTTP429(a, opts, err429) {
			t.Fatal("codex same-model-failover=false 应 pause")
		}
		if pinned := http429RetryPinnedAuthIDFromMetadata(opts.Metadata); pinned != "codex-auth" {
			t.Fatalf("expected pinned auth id 'codex-auth', got %q", pinned)
		}
	})

	t.Run("vertex 429 默认 immediate_failover 不 pause", func(t *testing.T) {
		a := &Auth{
			ID:        "vertex-auth",
			Provider:  "vertex",
			Attributes: map[string]string{
				"same_model_failover":    "true",
				"http_429_routing_policy": "immediate_failover",
			},
		}
		opts := cliproxyexecutor.Options{Metadata: map[string]any{"_init": true}}
		if m.shouldPauseOnHTTP429(a, opts, err429) {
			t.Fatal("vertex immediate_failover 不应 pause")
		}
	})

	t.Run("非 429 错误不 pause", func(t *testing.T) {
		a := &Auth{ID: "any", Provider: "gemini", Attributes: map[string]string{"same_model_failover": "false"}}
		opts := cliproxyexecutor.Options{Metadata: map[string]any{"_init": true}}
		err500 := &retryAfterStatusError{status: http.StatusInternalServerError, message: "oops"}
		if m.shouldPauseOnHTTP429(a, opts, err500) {
			t.Fatal("500 错误不应 pause")
		}
	})

	t.Run("nil auth 不 pause", func(t *testing.T) {
		opts := cliproxyexecutor.Options{Metadata: map[string]any{"_init": true}}
		if m.shouldPauseOnHTTP429(nil, opts, err429) {
			t.Fatal("nil auth 不应 pause")
		}
	})
}

func TestShouldPauseOnHTTP429_RuntimeConfigFallback(t *testing.T) {
	smf := false
	cfg := &internalconfig.Config{
		HTTP429Routing: internalconfig.NormalizeHTTP429RoutingConfig(internalconfig.HTTP429RoutingConfig{
			Overrides: []internalconfig.HTTP429RoutingOverride{
				{Provider: "codex", SameModelFailover: &smf},
			},
		}),
	}
	m := newTestManagerFor429Routing(cfg)
	err429 := &retryAfterStatusError{status: http.StatusTooManyRequests, message: "rate limited"}

	a := &Auth{
		ID:       "codex-no-attr",
		Provider: "codex",
	}
	opts := cliproxyexecutor.Options{Metadata: map[string]any{"_init": true}}
	if !m.shouldPauseOnHTTP429(a, opts, err429) {
		t.Fatal("codex runtime config override same-model-failover=false 应 pause")
	}
}
