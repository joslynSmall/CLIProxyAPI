package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigOptional_ProviderRateLimitDefaults(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("port: 8317\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfigOptional(configPath, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional() error = %v", err)
	}
	if !cfg.ProviderRateLimit.EnabledOrDefault() {
		t.Fatal("provider-rate-limit.enabled should default to true")
	}
	if cfg.ProviderRateLimit.Scope != ProviderRateLimitScopeCredential {
		t.Fatalf("provider-rate-limit.scope = %q, want %q", cfg.ProviderRateLimit.Scope, ProviderRateLimitScopeCredential)
	}
	if cfg.ProviderRateLimit.RateLimit != DefaultProviderRateLimit {
		t.Fatalf("provider-rate-limit.rate-limit = %d, want %d", cfg.ProviderRateLimit.RateLimit, DefaultProviderRateLimit)
	}
	if cfg.ProviderRateLimit.RateWindowSeconds != DefaultProviderRateWindowSec {
		t.Fatalf("provider-rate-limit.rate-window-seconds = %d, want %d", cfg.ProviderRateLimit.RateWindowSeconds, DefaultProviderRateWindowSec)
	}
	if cfg.ProviderRateLimit.MaxStreamConcurrency != DefaultProviderMaxConcurrent {
		t.Fatalf("provider-rate-limit.max-stream-concurrency = %d, want %d", cfg.ProviderRateLimit.MaxStreamConcurrency, DefaultProviderMaxConcurrent)
	}
}

func TestLoadConfigOptional_ProviderRateLimitSanitize(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	content := []byte(`
provider-rate-limit:
  enabled: false
  scope: wrong-scope
  rate-limit: -1
  rate-window-seconds: -2
  max-stream-concurrency: -3
  reactive-base-delay-ms: -4
  reactive-max-delay-seconds: -5
  reactive-jitter-ms: -6
  overrides:
    - provider: " OpenAI-Compatibility "
      scope: invalid
      rate-limit: -9
    - auth-id: " auth-1 "
      scope: provider
`)
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfigOptional(configPath, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional() error = %v", err)
	}
	if cfg.ProviderRateLimit.EnabledOrDefault() {
		t.Fatal("provider-rate-limit.enabled should be false")
	}
	if cfg.ProviderRateLimit.Scope != ProviderRateLimitScopeCredential {
		t.Fatalf("provider-rate-limit.scope = %q, want %q", cfg.ProviderRateLimit.Scope, ProviderRateLimitScopeCredential)
	}
	if cfg.ProviderRateLimit.RateLimit != DefaultProviderRateLimit {
		t.Fatalf("provider-rate-limit.rate-limit = %d, want %d", cfg.ProviderRateLimit.RateLimit, DefaultProviderRateLimit)
	}
	if len(cfg.ProviderRateLimit.Overrides) != 2 {
		t.Fatalf("provider-rate-limit.overrides count = %d, want 2", len(cfg.ProviderRateLimit.Overrides))
	}
	if cfg.ProviderRateLimit.Overrides[0].Provider != "openai-compatibility" {
		t.Fatalf("override[0].provider = %q, want openai-compatibility", cfg.ProviderRateLimit.Overrides[0].Provider)
	}
	if cfg.ProviderRateLimit.Overrides[0].Scope != "" {
		t.Fatalf("override[0].scope = %q, want empty", cfg.ProviderRateLimit.Overrides[0].Scope)
	}
	if cfg.ProviderRateLimit.Overrides[0].RateLimit != 0 {
		t.Fatalf("override[0].rate-limit = %d, want 0", cfg.ProviderRateLimit.Overrides[0].RateLimit)
	}
	if cfg.ProviderRateLimit.Overrides[1].AuthID != "auth-1" {
		t.Fatalf("override[1].auth-id = %q, want auth-1", cfg.ProviderRateLimit.Overrides[1].AuthID)
	}
	if cfg.ProviderRateLimit.Overrides[1].Scope != ProviderRateLimitScopeProvider {
		t.Fatalf("override[1].scope = %q, want %q", cfg.ProviderRateLimit.Overrides[1].Scope, ProviderRateLimitScopeProvider)
	}
}

func TestNormalizeProviderRateLimitConfig_InvalidScope(t *testing.T) {
	_, err := NormalizeProviderRateLimitConfig(ProviderRateLimitConfig{
		Scope: "invalid",
	})
	if err == nil {
		t.Fatal("expected invalid scope error")
	}
}
