package config

import "testing"

func TestNormalizeAPIKeyEntries(t *testing.T) {
	entries := []APIKeyEntry{
		{
			APIKey:           "  k1  ",
			AllowedSuppliers: []string{"claude-api-key:https://api.anthropic.com/", "claude-api-key:https://api.anthropic.com"},
			AllowedModels:    []string{" Models/Claude-3.7 ", "claude-3.7"},
		},
		{
			APIKey: "k1", // duplicate, should be dropped
		},
		{
			APIKey: "k2",
		},
		{
			APIKey: "   ",
		},
	}

	normalized := NormalizeAPIKeyEntries(entries)
	if len(normalized) != 2 {
		t.Fatalf("normalized len = %d, want 2", len(normalized))
	}
	if normalized[0].APIKey != "k1" {
		t.Fatalf("first key = %q, want k1", normalized[0].APIKey)
	}
	if got := normalized[0].AllowedSuppliers; len(got) != 1 || got[0] != "claude-api-key:https://api.anthropic.com" {
		t.Fatalf("allowed suppliers = %#v, want [claude-api-key:https://api.anthropic.com]", got)
	}
	if got := normalized[0].AllowedModels; len(got) != 1 || got[0] != "claude-3.7" {
		t.Fatalf("allowed models = %#v, want [claude-3.7]", got)
	}
}

func TestSanitizeAPIKeyEntries_DoesNotFallbackFromLegacyAPIKeys(t *testing.T) {
	cfg := &Config{
		SDKConfig: SDKConfig{
			APIKeyEntries: nil,
		},
	}

	cfg.SanitizeAPIKeyEntries()
	if len(cfg.APIKeyEntries) != 0 {
		t.Fatalf("APIKeyEntries = %#v, want nil", cfg.APIKeyEntries)
	}
}
