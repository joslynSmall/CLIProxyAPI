package synthesizer

import (
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestInjectHTTP429RoutingAttrs_NilConfig(t *testing.T) {
	attrs := map[string]string{}
	injectHTTP429RoutingAttrs(nil, attrs, "gemini", "")
	if len(attrs) != 0 {
		t.Fatalf("expected empty attrs with nil config, got %v", attrs)
	}
}

func TestInjectHTTP429RoutingAttrs_NilAttrs(t *testing.T) {
	cfg := &config.Config{HTTP429Routing: config.HTTP429RoutingConfig{}}
	injectHTTP429RoutingAttrs(cfg, nil, "gemini", "")
}

func TestInjectHTTP429RoutingAttrs_DefaultsWhenNoOverride(t *testing.T) {
	cfg := &config.Config{HTTP429Routing: config.NormalizeHTTP429RoutingConfig(config.HTTP429RoutingConfig{})}
	attrs := map[string]string{}
	injectHTTP429RoutingAttrs(cfg, attrs, "gemini", "")
	if attrs["same_model_failover"] != "true" {
		t.Fatalf("expected same_model_failover=true, got %q", attrs["same_model_failover"])
	}
	if attrs["http_429_routing_policy"] != config.HTTP429RoutingPolicyImmediateFailover {
		t.Fatalf("expected %q, got %q", config.HTTP429RoutingPolicyImmediateFailover, attrs["http_429_routing_policy"])
	}
}

func TestInjectHTTP429RoutingAttrs_OverrideMatch(t *testing.T) {
	smf := false
	cfg := &config.Config{HTTP429Routing: config.NormalizeHTTP429RoutingConfig(config.HTTP429RoutingConfig{
		Overrides: []config.HTTP429RoutingOverride{
			{Provider: "gemini", SameModelFailover: &smf, RoutingPolicy: config.HTTP429RoutingPolicyFailoverAfterCooldownWindow},
		},
	})}
	attrs := map[string]string{}
	injectHTTP429RoutingAttrs(cfg, attrs, "gemini", "")
	if attrs["same_model_failover"] != "false" {
		t.Fatalf("expected same_model_failover=false, got %q", attrs["same_model_failover"])
	}
	if attrs["http_429_routing_policy"] != config.HTTP429RoutingPolicyFailoverAfterCooldownWindow {
		t.Fatalf("expected %q, got %q", config.HTTP429RoutingPolicyFailoverAfterCooldownWindow, attrs["http_429_routing_policy"])
	}
}

func TestInjectHTTP429RoutingAttrs_DoesNotOverwriteExisting(t *testing.T) {
	cfg := &config.Config{HTTP429Routing: config.NormalizeHTTP429RoutingConfig(config.HTTP429RoutingConfig{})}
	attrs := map[string]string{
		"same_model_failover":   "false",
		"http_429_routing_policy": config.HTTP429RoutingPolicyFailoverAfterCooldownWindow,
	}
	injectHTTP429RoutingAttrs(cfg, attrs, "gemini", "")
	if attrs["same_model_failover"] != "false" {
		t.Fatalf("expected existing value preserved, got %q", attrs["same_model_failover"])
	}
	if attrs["http_429_routing_policy"] != config.HTTP429RoutingPolicyFailoverAfterCooldownWindow {
		t.Fatalf("expected existing value preserved, got %q", attrs["http_429_routing_policy"])
	}
}

func TestInjectHTTP429RoutingAttrs_PartialExisting(t *testing.T) {
	cfg := &config.Config{HTTP429Routing: config.NormalizeHTTP429RoutingConfig(config.HTTP429RoutingConfig{})}
	attrs := map[string]string{
		"same_model_failover": "false",
	}
	injectHTTP429RoutingAttrs(cfg, attrs, "gemini", "")
	if attrs["same_model_failover"] != "false" {
		t.Fatalf("expected existing value preserved, got %q", attrs["same_model_failover"])
	}
	if attrs["http_429_routing_policy"] != config.HTTP429RoutingPolicyImmediateFailover {
		t.Fatalf("expected %q injected for missing key, got %q", config.HTTP429RoutingPolicyImmediateFailover, attrs["http_429_routing_policy"])
	}
}

func TestInjectHTTP429RoutingAttrs_ProviderKeyMatch(t *testing.T) {
	smf := false
	cfg := &config.Config{HTTP429Routing: config.NormalizeHTTP429RoutingConfig(config.HTTP429RoutingConfig{
		Overrides: []config.HTTP429RoutingOverride{
			{Provider: "openai-compatibility", ProviderKey: "deepseek", SameModelFailover: &smf, RoutingPolicy: config.HTTP429RoutingPolicyFailoverAfterCooldownWindow},
		},
	})}
	attrs := map[string]string{}
	injectHTTP429RoutingAttrs(cfg, attrs, "openai-compatibility", "deepseek")
	if attrs["same_model_failover"] != "false" {
		t.Fatalf("expected same_model_failover=false, got %q", attrs["same_model_failover"])
	}
}

func TestConfigSynthesizer_GeminiKeys_429AttrsInjected(t *testing.T) {
	smf := false
	cfg := &config.Config{
		HTTP429Routing: config.NormalizeHTTP429RoutingConfig(config.HTTP429RoutingConfig{
			Overrides: []config.HTTP429RoutingOverride{
				{Provider: "gemini", SameModelFailover: &smf, RoutingPolicy: config.HTTP429RoutingPolicyFailoverAfterCooldownWindow},
			},
		}),
		GeminiKey: []config.GeminiKey{{APIKey: "g-key"}},
	}
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{Config: cfg, Now: time.Now(), IDGenerator: NewStableIDGenerator()}
	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth, got %d", len(auths))
	}
	if auths[0].Attributes["same_model_failover"] != "false" {
		t.Fatalf("expected same_model_failover=false for gemini, got %q", auths[0].Attributes["same_model_failover"])
	}
	if auths[0].Attributes["http_429_routing_policy"] != config.HTTP429RoutingPolicyFailoverAfterCooldownWindow {
		t.Fatalf("expected %q for gemini, got %q", config.HTTP429RoutingPolicyFailoverAfterCooldownWindow, auths[0].Attributes["http_429_routing_policy"])
	}
}

func TestConfigSynthesizer_ClaudeKeys_429AttrsInjected(t *testing.T) {
	cfg := &config.Config{
		HTTP429Routing: config.NormalizeHTTP429RoutingConfig(config.HTTP429RoutingConfig{}),
		ClaudeKey: []config.ClaudeKey{{APIKey: "c-key"}},
	}
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{Config: cfg, Now: time.Now(), IDGenerator: NewStableIDGenerator()}
	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth, got %d", len(auths))
	}
	if auths[0].Attributes["same_model_failover"] != "true" {
		t.Fatalf("expected same_model_failover=true for claude, got %q", auths[0].Attributes["same_model_failover"])
	}
	if auths[0].Attributes["http_429_routing_policy"] != config.HTTP429RoutingPolicyImmediateFailover {
		t.Fatalf("expected %q for claude, got %q", config.HTTP429RoutingPolicyImmediateFailover, auths[0].Attributes["http_429_routing_policy"])
	}
}

func TestConfigSynthesizer_CodexKeys_429AttrsInjected(t *testing.T) {
	cfg := &config.Config{
		HTTP429Routing: config.NormalizeHTTP429RoutingConfig(config.HTTP429RoutingConfig{}),
		CodexKey: []config.CodexKey{{APIKey: "cx-key"}},
	}
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{Config: cfg, Now: time.Now(), IDGenerator: NewStableIDGenerator()}
	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth, got %d", len(auths))
	}
	if auths[0].Attributes["same_model_failover"] != "true" {
		t.Fatalf("expected same_model_failover=true for codex, got %q", auths[0].Attributes["same_model_failover"])
	}
}

func TestConfigSynthesizer_VertexKeys_429AttrsInjected(t *testing.T) {
	cfg := &config.Config{
		HTTP429Routing: config.NormalizeHTTP429RoutingConfig(config.HTTP429RoutingConfig{}),
		VertexCompatAPIKey: []config.VertexCompatKey{{APIKey: "v-key", BaseURL: "https://vertex.api"}},
	}
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{Config: cfg, Now: time.Now(), IDGenerator: NewStableIDGenerator()}
	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth, got %d", len(auths))
	}
	if auths[0].Attributes["same_model_failover"] != "true" {
		t.Fatalf("expected same_model_failover=true for vertex, got %q", auths[0].Attributes["same_model_failover"])
	}
}

func TestConfigSynthesizer_OpenAICompat_429OldFieldOverridesGlobal(t *testing.T) {
	cfg := &config.Config{
		HTTP429Routing: config.NormalizeHTTP429RoutingConfig(config.HTTP429RoutingConfig{
			RoutingPolicy: config.HTTP429RoutingPolicyImmediateFailover,
		}),
		OpenAICompatibility: []config.OpenAICompatibility{
			{
				Name:              "deepseek",
				BaseURL:           "https://api.deepseek.com",
				HTTP429RoutingPolicy: "failover_after_cooldown_window",
				APIKeyEntries:     []config.OpenAICompatibilityAPIKey{{APIKey: "ds-key"}},
			},
		},
	}
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{Config: cfg, Now: time.Now(), IDGenerator: NewStableIDGenerator()}
	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth, got %d", len(auths))
	}
	if auths[0].Attributes["http_429_routing_policy"] != config.HTTP429RoutingPolicyFailoverAfterCooldownWindow {
		t.Fatalf("旧字段应覆盖全局默认，expected %q, got %q", config.HTTP429RoutingPolicyFailoverAfterCooldownWindow, auths[0].Attributes["http_429_routing_policy"])
	}
}

func TestConfigSynthesizer_OpenAICompat_Fallback_429OldFieldOverridesGlobal(t *testing.T) {
	cfg := &config.Config{
		HTTP429Routing: config.NormalizeHTTP429RoutingConfig(config.HTTP429RoutingConfig{
			RoutingPolicy: config.HTTP429RoutingPolicyImmediateFailover,
		}),
		OpenAICompatibility: []config.OpenAICompatibility{
			{
				Name:                 "grok",
				BaseURL:              "https://api.x.ai",
				HTTP429RoutingPolicy: "failover_after_cooldown_window",
			},
		},
	}
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{Config: cfg, Now: time.Now(), IDGenerator: NewStableIDGenerator()}
	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth, got %d", len(auths))
	}
	if auths[0].Attributes["http_429_routing_policy"] != config.HTTP429RoutingPolicyFailoverAfterCooldownWindow {
		t.Fatalf("fallback 路径旧字段应覆盖全局默认，expected %q, got %q", config.HTTP429RoutingPolicyFailoverAfterCooldownWindow, auths[0].Attributes["http_429_routing_policy"])
	}
}

func TestInjectHTTP429RoutingAttrs_AllProvidersGetDefaults(t *testing.T) {
	cfg := &config.Config{
		HTTP429Routing: config.NormalizeHTTP429RoutingConfig(config.HTTP429RoutingConfig{}),
		GeminiKey:           []config.GeminiKey{{APIKey: "g-key"}},
		ClaudeKey:           []config.ClaudeKey{{APIKey: "c-key"}},
		CodexKey:            []config.CodexKey{{APIKey: "cx-key"}},
		VertexCompatAPIKey:  []config.VertexCompatKey{{APIKey: "v-key", BaseURL: "https://vertex.api"}},
		OpenAICompatibility: []config.OpenAICompatibility{{Name: "test", BaseURL: "https://test.api", APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "ds-key"}}}},
	}
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{Config: cfg, Now: time.Now(), IDGenerator: NewStableIDGenerator()}
	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	providerAttrs := map[string]map[string]string{}
	for _, a := range auths {
		providerAttrs[a.Provider] = a.Attributes
	}

	for _, provider := range []string{"gemini", "claude", "codex", "vertex", "test"} {
		attrs, ok := providerAttrs[provider]
		if !ok {
			t.Fatalf("provider %s not found in synthesized auths", provider)
		}
		if attrs["same_model_failover"] != "true" {
			t.Errorf("provider %s: expected same_model_failover=true, got %q", provider, attrs["same_model_failover"])
		}
		if attrs["http_429_routing_policy"] != config.HTTP429RoutingPolicyImmediateFailover {
			t.Errorf("provider %s: expected %q, got %q", provider, config.HTTP429RoutingPolicyImmediateFailover, attrs["http_429_routing_policy"])
		}
	}
}
