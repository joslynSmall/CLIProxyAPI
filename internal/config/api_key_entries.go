package config

import (
	"strings"
)

// NormalizeAPIKeyEntries trims and deduplicates entries by API key.
// First occurrence wins when duplicate api-key values are present.
func NormalizeAPIKeyEntries(entries []APIKeyEntry) []APIKeyEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]APIKeyEntry, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, raw := range entries {
		key := strings.TrimSpace(raw.APIKey)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		entry := APIKeyEntry{
			APIKey:           key,
			AllowedSuppliers: normalizeAPIKeyEntrySuppliers(raw.AllowedSuppliers),
			AllowedModels:    normalizeAPIKeyEntryModels(raw.AllowedModels),
		}
		out = append(out, entry)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeAPIKeyEntrySuppliers(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		normalized := normalizeSupplierKeyLocal(raw)
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

func normalizeAPIKeyEntryModels(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		normalized := strings.ToLower(strings.TrimSpace(raw))
		normalized = strings.TrimPrefix(normalized, "models/")
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

func normalizeSupplierKeyLocal(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	kind := strings.ToLower(strings.TrimSpace(parts[0]))
	value := strings.TrimSpace(parts[1])
	switch kind {
	case "openai-compatibility":
		value = strings.ToLower(value)
		if value == "" {
			return ""
		}
		return kind + ":" + value
	case "claude-api-key", "codex-api-key":
		value = strings.ToLower(strings.TrimRight(value, "/"))
		if value == "" {
			value = "default"
		}
		return kind + ":" + value
	default:
		return ""
	}
}

// SanitizeAPIKeyEntries normalizes top-level api-key-entries only.
func (cfg *Config) SanitizeAPIKeyEntries() {
	if cfg == nil {
		return
	}
	cfg.APIKeyEntries = NormalizeAPIKeyEntries(cfg.APIKeyEntries)
}
