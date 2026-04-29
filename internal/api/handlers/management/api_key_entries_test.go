package management

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestNormalizeAPIKeyEntriesStrict(t *testing.T) {
	entries := []config.APIKeyEntry{
		{
			APIKey:           "k1",
			AllowedSuppliers: []string{"claude-api-key:https://api.anthropic.com/"},
			AllowedModels:    []string{" models/Claude-3.7 "},
		},
		{
			APIKey: "k1",
		},
	}

	normalized, err := normalizeAPIKeyEntriesStrict(entries)
	if err != nil {
		t.Fatalf("normalizeAPIKeyEntriesStrict() err = %v", err)
	}
	if len(normalized) != 1 {
		t.Fatalf("normalized len = %d, want 1", len(normalized))
	}
	if got := normalized[0].AllowedSuppliers; len(got) != 1 || got[0] != "claude-api-key:https://api.anthropic.com" {
		t.Fatalf("allowed suppliers = %#v, want [claude-api-key:https://api.anthropic.com]", got)
	}
	if got := normalized[0].AllowedModels; len(got) != 1 || got[0] != "claude-3.7" {
		t.Fatalf("allowed models = %#v, want [claude-3.7]", got)
	}
}

func TestNormalizeAPIKeyEntriesStrictInvalidSupplier(t *testing.T) {
	entries := []config.APIKeyEntry{
		{
			APIKey:           "k1",
			AllowedSuppliers: []string{"gemini:abc"},
		},
	}
	if _, err := normalizeAPIKeyEntriesStrict(entries); err == nil {
		t.Fatal("expected error for invalid supplier key")
	}
}

func TestCollectSupplierOptions(t *testing.T) {
	cfg := &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{
			{Name: "MyGW"},
			{Name: "MyGW"},
		},
		ClaudeKey: []config.ClaudeKey{
			{BaseURL: "https://api.anthropic.com/"},
		},
		CodexKey: []config.CodexKey{
			{BaseURL: ""},
		},
	}
	auths := []*coreauth.Auth{
		{
			Provider: "gemini-cli",
			Attributes: map[string]string{
				"auth_kind": "oauth",
				"source":    "/tmp/auths/gemini.json",
			},
		},
	}

	options := collectSupplierOptions(cfg, auths)
	want := map[string]struct{}{
		"openai-compatibility:mygw":                {},
		"claude-api-key:https://api.anthropic.com": {},
		"codex-api-key:default":                    {},
		"oauth:gemini-cli":                         {},
	}
	if len(options) != len(want) {
		t.Fatalf("options len = %d, want %d, options=%#v", len(options), len(want), options)
	}
	for _, option := range options {
		if _, ok := want[option]; !ok {
			t.Fatalf("unexpected option %q", option)
		}
	}
}
