package apikeyscope

import (
	"fmt"
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

const (
	ScopeAllowedSuppliersMetadataKey = "scope_allowed_suppliers"
	ScopeAllowedModelsMetadataKey    = "scope_allowed_models"
	ScopeSourceMetadataKey           = "scope_source"

	scopeListSeparator = ","
)

// EncodeScopeValues serializes a list for metadata transmission.
func EncodeScopeValues(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, scopeListSeparator)
}

// DecodeScopeValues parses metadata list payload into normalized values.
func DecodeScopeValues(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	items := strings.Split(raw, scopeListSeparator)
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NormalizeBaseURL canonicalizes configured base URLs for scope matching.
func NormalizeBaseURL(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	trimmed = strings.TrimRight(trimmed, "/")
	return trimmed
}

func supplierScopeKey(kind, value string) string {
	return strings.TrimSpace(kind) + ":" + strings.TrimSpace(value)
}

// SupplierKeyForOpenAICompatibility builds the canonical supplier key for one openai-compatibility entry.
func SupplierKeyForOpenAICompatibility(name string) string {
	normalizedName := strings.ToLower(strings.TrimSpace(name))
	if normalizedName == "" {
		normalizedName = "openai-compatibility"
	}
	return supplierScopeKey("openai-compatibility", normalizedName)
}

// SupplierKeyForClaudeBaseURL builds the canonical supplier key for one claude-api-key entry.
func SupplierKeyForClaudeBaseURL(baseURL string) string {
	normalizedBase := NormalizeBaseURL(baseURL)
	if normalizedBase == "" {
		normalizedBase = "default"
	}
	return supplierScopeKey("claude-api-key", normalizedBase)
}

// SupplierKeyForCodexBaseURL builds the canonical supplier key for one codex-api-key entry.
func SupplierKeyForCodexBaseURL(baseURL string) string {
	normalizedBase := NormalizeBaseURL(baseURL)
	if normalizedBase == "" {
		normalizedBase = "default"
	}
	return supplierScopeKey("codex-api-key", normalizedBase)
}

// SupplierKeyForOAuthProvider builds the canonical supplier key for one OAuth provider channel.
func SupplierKeyForOAuthProvider(provider string) string {
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	if normalizedProvider == "" {
		normalizedProvider = "unknown"
	}
	return supplierScopeKey("oauth", normalizedProvider)
}

// NormalizeSupplierKey validates and canonicalizes one supplier key string.
func NormalizeSupplierKey(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid supplier key %q: missing ':'", raw)
	}
	kind := strings.ToLower(strings.TrimSpace(parts[0]))
	value := strings.TrimSpace(parts[1])
	switch kind {
	case "openai-compatibility":
		if value == "" {
			return "", fmt.Errorf("invalid supplier key %q: openai-compatibility name is empty", raw)
		}
		return SupplierKeyForOpenAICompatibility(value), nil
	case "claude-api-key":
		if value == "" {
			return SupplierKeyForClaudeBaseURL(""), nil
		}
		return SupplierKeyForClaudeBaseURL(value), nil
	case "codex-api-key":
		if value == "" {
			return SupplierKeyForCodexBaseURL(""), nil
		}
		return SupplierKeyForCodexBaseURL(value), nil
	case "oauth":
		if value == "" {
			return "", fmt.Errorf("invalid supplier key %q: oauth provider is empty", raw)
		}
		return SupplierKeyForOAuthProvider(value), nil
	default:
		return "", fmt.Errorf("invalid supplier key %q: unsupported kind %q", raw, kind)
	}
}

// NormalizeSupplierKeys canonicalizes and deduplicates supplier keys.
func NormalizeSupplierKeys(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		normalized, err := NormalizeSupplierKey(raw)
		if err != nil {
			return nil, err
		}
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// NormalizeModelKey canonicalizes a model identifier for set comparisons.
func NormalizeModelKey(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	trimmed = strings.TrimPrefix(trimmed, "models/")
	return trimmed
}

// NormalizeModelKeys deduplicates and canonicalizes model identifiers.
func NormalizeModelKeys(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		normalized := NormalizeModelKey(raw)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SupplierKeyFromAuth derives the canonical supplier key for auth entries.
func SupplierKeyFromAuth(auth *coreauth.Auth) string {
	if auth == nil || auth.Attributes == nil {
		return ""
	}
	source := strings.ToLower(strings.TrimSpace(auth.Attributes["source"]))
	authKind := strings.ToLower(strings.TrimSpace(auth.Attributes["auth_kind"]))
	baseURL := auth.Attributes["base_url"]

	if authKind == "oauth" {
		provider := strings.TrimSpace(auth.Provider)
		if provider == "" {
			provider = strings.TrimSpace(auth.Attributes["provider_key"])
		}
		return SupplierKeyForOAuthProvider(provider)
	}

	switch {
	case strings.HasPrefix(source, "config:claude["):
		return SupplierKeyForClaudeBaseURL(baseURL)
	case strings.HasPrefix(source, "config:codex["):
		return SupplierKeyForCodexBaseURL(baseURL)
	}

	compatName, hasCompatName := auth.Attributes["compat_name"]
	if hasCompatName || strings.HasPrefix(source, "config:openai-compatibility") {
		name := strings.TrimSpace(compatName)
		if name == "" {
			name = strings.TrimSpace(auth.Attributes["provider_key"])
		}
		if name == "" {
			name = strings.TrimSpace(auth.Provider)
		}
		return SupplierKeyForOpenAICompatibility(name)
	}

	return ""
}
