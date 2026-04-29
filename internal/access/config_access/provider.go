package configaccess

import (
	"context"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/apikeyscope"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

// Register ensures the config-access provider is available to the access manager.
func Register(cfg *sdkconfig.SDKConfig) {
	if cfg == nil {
		sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey)
		return
	}

	entries := normalizeEntries(cfg)
	if len(entries) == 0 {
		sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey)
		return
	}

	sdkaccess.RegisterProvider(
		sdkaccess.AccessProviderTypeConfigAPIKey,
		newProvider(sdkaccess.DefaultAccessProviderName, entries),
	)
}

type provider struct {
	name string
	keys map[string]scopedEntry
}

type scopedEntry struct {
	allowedSuppliers []string
	allowedModels    []string
	scopeSource      string
}

func newProvider(name string, entries map[string]scopedEntry) *provider {
	providerName := strings.TrimSpace(name)
	if providerName == "" {
		providerName = sdkaccess.DefaultAccessProviderName
	}
	return &provider{name: providerName, keys: entries}
}

func (p *provider) Identifier() string {
	if p == nil || p.name == "" {
		return sdkaccess.DefaultAccessProviderName
	}
	return p.name
}

func (p *provider) Authenticate(_ context.Context, r *http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
	if p == nil {
		return nil, sdkaccess.NewNotHandledError()
	}
	if len(p.keys) == 0 {
		return nil, sdkaccess.NewNotHandledError()
	}
	authHeader := r.Header.Get("Authorization")
	authHeaderGoogle := r.Header.Get("X-Goog-Api-Key")
	authHeaderAnthropic := r.Header.Get("X-Api-Key")
	queryKey := ""
	queryAuthToken := ""
	if r.URL != nil {
		queryKey = r.URL.Query().Get("key")
		queryAuthToken = r.URL.Query().Get("auth_token")
	}
	if authHeader == "" && authHeaderGoogle == "" && authHeaderAnthropic == "" && queryKey == "" && queryAuthToken == "" {
		return nil, sdkaccess.NewNoCredentialsError()
	}

	apiKey := extractBearerToken(authHeader)

	candidates := []struct {
		value  string
		source string
	}{
		{apiKey, "authorization"},
		{authHeaderGoogle, "x-goog-api-key"},
		{authHeaderAnthropic, "x-api-key"},
		{queryKey, "query-key"},
		{queryAuthToken, "query-auth-token"},
	}

	for _, candidate := range candidates {
		if candidate.value == "" {
			continue
		}
		entry, ok := p.keys[candidate.value]
		if !ok {
			continue
		}
		metadata := map[string]string{
			"source": candidate.source,
		}
		if entry.scopeSource != "" {
			metadata[apikeyscope.ScopeSourceMetadataKey] = entry.scopeSource
		}
		if encoded := apikeyscope.EncodeScopeValues(entry.allowedSuppliers); encoded != "" {
			metadata[apikeyscope.ScopeAllowedSuppliersMetadataKey] = encoded
		}
		if encoded := apikeyscope.EncodeScopeValues(entry.allowedModels); encoded != "" {
			metadata[apikeyscope.ScopeAllowedModelsMetadataKey] = encoded
		}
		return &sdkaccess.Result{
			Provider:  p.Identifier(),
			Principal: candidate.value,
			Metadata:  metadata,
		}, nil
	}

	return nil, sdkaccess.NewInvalidCredentialError()
}

func extractBearerToken(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return header
	}
	if strings.ToLower(parts[0]) != "bearer" {
		return header
	}
	return strings.TrimSpace(parts[1])
}

func normalizeEntries(cfg *sdkconfig.SDKConfig) map[string]scopedEntry {
	if cfg == nil || len(cfg.APIKeyEntries) == 0 {
		return nil
	}
	out := make(map[string]scopedEntry, len(cfg.APIKeyEntries))
	for _, raw := range cfg.APIKeyEntries {
		key := strings.TrimSpace(raw.APIKey)
		if key == "" {
			continue
		}
		if _, exists := out[key]; exists {
			continue
		}

		suppliers, err := apikeyscope.NormalizeSupplierKeys(raw.AllowedSuppliers)
		if err != nil {
			// Invalid supplier input is ignored here to keep auth availability robust.
			suppliers = nil
		}
		out[key] = scopedEntry{
			allowedSuppliers: suppliers,
			allowedModels:    apikeyscope.NormalizeModelKeys(raw.AllowedModels),
			scopeSource:      "api-key-entries",
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
