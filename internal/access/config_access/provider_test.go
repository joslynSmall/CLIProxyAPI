package configaccess

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/apikeyscope"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func TestProviderAuthenticateWithScopedEntries(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{
		APIKeyEntries: []sdkconfig.APIKeyEntry{
			{
				APIKey:           "k1",
				AllowedSuppliers: []string{"claude-api-key:https://api.anthropic.com/"},
				AllowedModels:    []string{" models/Claude-3.7 "},
			},
		},
	}

	p := newProvider("test-provider", normalizeEntries(cfg))
	req := &http.Request{
		Header: make(http.Header),
		URL:    &url.URL{},
	}
	req.Header.Set("Authorization", "Bearer k1")

	result, authErr := p.Authenticate(context.Background(), req)
	if authErr != nil {
		t.Fatalf("Authenticate() authErr = %v, want nil", authErr)
	}
	if result == nil {
		t.Fatal("Authenticate() result is nil")
	}
	if result.Principal != "k1" {
		t.Fatalf("principal = %q, want %q", result.Principal, "k1")
	}
	if result.Metadata[apikeyscope.ScopeSourceMetadataKey] != "api-key-entries" {
		t.Fatalf("scope source = %q, want %q", result.Metadata[apikeyscope.ScopeSourceMetadataKey], "api-key-entries")
	}
	if got := result.Metadata[apikeyscope.ScopeAllowedSuppliersMetadataKey]; got != "claude-api-key:https://api.anthropic.com" {
		t.Fatalf("allowed suppliers = %q, want %q", got, "claude-api-key:https://api.anthropic.com")
	}
	if got := result.Metadata[apikeyscope.ScopeAllowedModelsMetadataKey]; got != "claude-3.7" {
		t.Fatalf("allowed models = %q, want %q", got, "claude-3.7")
	}
}

func TestProviderNormalizeEntries_NoLegacyFallback(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{
		APIKeyEntries: nil,
	}
	got := normalizeEntries(cfg)
	if len(got) != 0 {
		t.Fatalf("normalizeEntries() = %#v, want nil", got)
	}
}
