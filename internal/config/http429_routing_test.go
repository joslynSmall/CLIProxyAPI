package config

import "testing"

func TestNormalizeHTTP429RoutingPolicy(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"", HTTP429RoutingPolicyImmediateFailover},
		{"immediate_failover", HTTP429RoutingPolicyImmediateFailover},
		{"failover_after_cooldown_window", HTTP429RoutingPolicyFailoverAfterCooldownWindow},
		{"IMMEDIATE_FAILOVER", HTTP429RoutingPolicyImmediateFailover},
		{"FAILOVER_AFTER_COOLDOWN_WINDOW", HTTP429RoutingPolicyFailoverAfterCooldownWindow},
		{" immediate_failover ", HTTP429RoutingPolicyImmediateFailover},
		{" unknown_policy ", HTTP429RoutingPolicyImmediateFailover},
		{"ImMeDiAtE_FaIlOvEr", HTTP429RoutingPolicyImmediateFailover},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			if got := NormalizeHTTP429RoutingPolicy(tt.raw); got != tt.want {
				t.Fatalf("NormalizeHTTP429RoutingPolicy(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestHTTP429RoutingConfig_SameModelFailoverOrDefault(t *testing.T) {
	t.Run("nil defaults to true", func(t *testing.T) {
		c := HTTP429RoutingConfig{}
		if !c.SameModelFailoverOrDefault() {
			t.Fatal("expected default true when SameModelFailover is nil")
		}
	})
	t.Run("explicit true", func(t *testing.T) {
		c := HTTP429RoutingConfig{SameModelFailover: boolPtr(true)}
		if !c.SameModelFailoverOrDefault() {
			t.Fatal("expected true")
		}
	})
	t.Run("explicit false", func(t *testing.T) {
		c := HTTP429RoutingConfig{SameModelFailover: boolPtr(false)}
		if c.SameModelFailoverOrDefault() {
			t.Fatal("expected false")
		}
	})
}

func TestHTTP429RoutingConfig_RoutingPolicyOrDefault(t *testing.T) {
	t.Run("empty defaults to immediate_failover", func(t *testing.T) {
		c := HTTP429RoutingConfig{}
		if got := c.RoutingPolicyOrDefault(); got != HTTP429RoutingPolicyImmediateFailover {
			t.Fatalf("expected %q, got %q", HTTP429RoutingPolicyImmediateFailover, got)
		}
	})
	t.Run("valid cooldown policy", func(t *testing.T) {
		c := HTTP429RoutingConfig{RoutingPolicy: HTTP429RoutingPolicyFailoverAfterCooldownWindow}
		if got := c.RoutingPolicyOrDefault(); got != HTTP429RoutingPolicyFailoverAfterCooldownWindow {
			t.Fatalf("expected %q, got %q", HTTP429RoutingPolicyFailoverAfterCooldownWindow, got)
		}
	})
	t.Run("invalid policy normalized to immediate_failover", func(t *testing.T) {
		c := HTTP429RoutingConfig{RoutingPolicy: "bogus"}
		if got := c.RoutingPolicyOrDefault(); got != HTTP429RoutingPolicyImmediateFailover {
			t.Fatalf("expected %q, got %q", HTTP429RoutingPolicyImmediateFailover, got)
		}
	})
}

func TestHTTP429RoutingConfig_ResolveOverride(t *testing.T) {
	c := HTTP429RoutingConfig{
		SameModelFailover: boolPtr(true),
		RoutingPolicy:     HTTP429RoutingPolicyImmediateFailover,
		Overrides: []HTTP429RoutingOverride{
			{Provider: "gemini", SameModelFailover: boolPtr(false), RoutingPolicy: HTTP429RoutingPolicyFailoverAfterCooldownWindow},
			{Provider: "openai-compatibility", ProviderKey: "deepseek", SameModelFailover: boolPtr(false)},
			{Provider: "openai-compatibility", ProviderKey: "grok", RoutingPolicy: HTTP429RoutingPolicyFailoverAfterCooldownWindow},
			{Provider: "openai-compatibility", SameModelFailover: boolPtr(true)},
		},
	}

	t.Run("provider+providerKey 精确匹配优先于 provider-only", func(t *testing.T) {
		smf, rp, found := c.ResolveOverride("openai-compatibility", "deepseek")
		if !found {
			t.Fatal("expected found")
		}
		if smf {
			t.Fatal("expected same-model-failover=false from provider+key override")
		}
		if rp != HTTP429RoutingPolicyImmediateFailover {
			t.Fatalf("expected policy %q from provider+key override (未设 policy 应走默认), got %q", HTTP429RoutingPolicyImmediateFailover, rp)
		}
	})

	t.Run("provider-only 匹配在无 providerKey 精确匹配时生效", func(t *testing.T) {
		smf, rp, found := c.ResolveOverride("openai-compatibility", "unknown-compat")
		if !found {
			t.Fatal("expected found via provider-only fallback")
		}
		if !smf {
			t.Fatal("expected same-model-failover=true from provider-only override")
		}
		if rp != HTTP429RoutingPolicyImmediateFailover {
			t.Fatalf("expected policy %q from provider-only override, got %q", HTTP429RoutingPolicyImmediateFailover, rp)
		}
	})

	t.Run("gemini 无 providerKey 的 override", func(t *testing.T) {
		smf, rp, found := c.ResolveOverride("gemini", "")
		if !found {
			t.Fatal("expected found")
		}
		if smf {
			t.Fatal("expected same-model-failover=false")
		}
		if rp != HTTP429RoutingPolicyFailoverAfterCooldownWindow {
			t.Fatalf("expected %q, got %q", HTTP429RoutingPolicyFailoverAfterCooldownWindow, rp)
		}
	})

	t.Run("无匹配 override 返回 found=false", func(t *testing.T) {
		_, _, found := c.ResolveOverride("claude", "")
		if found {
			t.Fatal("expected not found for claude")
		}
	})

	t.Run("大小写不敏感匹配", func(t *testing.T) {
		smf, _, found := c.ResolveOverride("GEMINI", "")
		if !found {
			t.Fatal("expected case-insensitive match")
		}
		if smf {
			t.Fatal("expected same-model-failover=false for GEMINI")
		}
	})

	t.Run("providerKey 大小写不敏感", func(t *testing.T) {
		smf, _, found := c.ResolveOverride("openai-compatibility", "DeepSeek")
		if !found {
			t.Fatal("expected case-insensitive providerKey match")
		}
		if smf {
			t.Fatal("expected same-model-failover=false for deepseek")
		}
	})

	t.Run("空 providerKey 不匹配有 providerKey 的 override", func(t *testing.T) {
		_, _, found := c.ResolveOverride("openai-compatibility", "")
		if !found {
			t.Fatal("expected provider-only fallback match")
		}
	})

	t.Run("grok override 只设 routingPolicy", func(t *testing.T) {
		smf, rp, found := c.ResolveOverride("openai-compatibility", "grok")
		if !found {
			t.Fatal("expected found")
		}
		if !smf {
			t.Fatal("expected same-model-failover=true (未设则默认)")
		}
		if rp != HTTP429RoutingPolicyFailoverAfterCooldownWindow {
			t.Fatalf("expected %q, got %q", HTTP429RoutingPolicyFailoverAfterCooldownWindow, rp)
		}
	})
}

func TestNormalizeHTTP429RoutingConfig(t *testing.T) {
	t.Run("nil SameModelFailover 默认为 true", func(t *testing.T) {
		out := NormalizeHTTP429RoutingConfig(HTTP429RoutingConfig{})
		if out.SameModelFailover == nil || !*out.SameModelFailover {
			t.Fatal("expected SameModelFailover to be *true after normalize")
		}
	})

	t.Run("RoutingPolicy 归一化", func(t *testing.T) {
		out := NormalizeHTTP429RoutingConfig(HTTP429RoutingConfig{RoutingPolicy: " FAILOVER_AFTER_COOLDOWN_WINDOW "})
		if out.RoutingPolicy != HTTP429RoutingPolicyFailoverAfterCooldownWindow {
			t.Fatalf("expected %q, got %q", HTTP429RoutingPolicyFailoverAfterCooldownWindow, out.RoutingPolicy)
		}
	})

	t.Run("override 的 provider/providerKey 大小写归一化", func(t *testing.T) {
		out := NormalizeHTTP429RoutingConfig(HTTP429RoutingConfig{
			Overrides: []HTTP429RoutingOverride{
				{Provider: " GEMINI ", ProviderKey: " MyKey ", RoutingPolicy: "IMMEDIATE_FAILOVER"},
			},
		})
		if len(out.Overrides) != 1 {
			t.Fatalf("expected 1 override, got %d", len(out.Overrides))
		}
		if out.Overrides[0].Provider != "gemini" {
			t.Fatalf("expected provider 'gemini', got %q", out.Overrides[0].Provider)
		}
		if out.Overrides[0].ProviderKey != "mykey" {
			t.Fatalf("expected providerKey 'mykey', got %q", out.Overrides[0].ProviderKey)
		}
		if out.Overrides[0].RoutingPolicy != HTTP429RoutingPolicyImmediateFailover {
			t.Fatalf("expected %q, got %q", HTTP429RoutingPolicyImmediateFailover, out.Overrides[0].RoutingPolicy)
		}
	})

	t.Run("invalid RoutingPolicy 回退到 immediate_failover", func(t *testing.T) {
		out := NormalizeHTTP429RoutingConfig(HTTP429RoutingConfig{RoutingPolicy: "garbage"})
		if out.RoutingPolicy != HTTP429RoutingPolicyImmediateFailover {
			t.Fatalf("expected %q, got %q", HTTP429RoutingPolicyImmediateFailover, out.RoutingPolicy)
		}
	})
}
