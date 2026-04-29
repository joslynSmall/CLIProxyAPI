package handlers

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/apikeyscope"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
)

type requestModelScope struct {
	allowedSuppliers map[string]struct{}
	allowedModels    map[string]struct{}
}

func (s requestModelScope) empty() bool {
	return len(s.allowedSuppliers) == 0 && len(s.allowedModels) == 0
}

// FilterModelsByAccessScope applies per-key supplier/model visibility limits from access metadata.
func (h *BaseAPIHandler) FilterModelsByAccessScope(c *gin.Context, models []map[string]any) []map[string]any {
	scope := scopeFromAccessMetadata(c)
	if scope.empty() {
		return models
	}

	modelSuppliers := h.buildModelSupplierIndex()
	filtered := make([]map[string]any, 0, len(models))
	for _, model := range models {
		keys := modelComparableKeys(model)
		if len(keys) == 0 {
			continue
		}
		if !scope.matchesModel(keys) {
			continue
		}
		if !scope.matchesSupplier(keys, modelSuppliers) {
			continue
		}
		filtered = append(filtered, model)
	}
	return filtered
}

func (h *BaseAPIHandler) buildModelSupplierIndex() map[string]map[string]struct{} {
	if h == nil || h.AuthManager == nil {
		return nil
	}
	auths := h.AuthManager.List()
	if len(auths) == 0 {
		return nil
	}

	modelRegistry := registry.GetGlobalRegistry()
	index := make(map[string]map[string]struct{}, 128)
	for _, auth := range auths {
		supplierKey := apikeyscope.SupplierKeyFromAuth(auth)
		if supplierKey == "" {
			continue
		}
		models := modelRegistry.GetModelsForClient(auth.ID)
		for _, model := range models {
			if model == nil {
				continue
			}
			keys := modelKeysFromInfo(model.ID, model.Name)
			for _, key := range keys {
				set := index[key]
				if set == nil {
					set = make(map[string]struct{}, 1)
					index[key] = set
				}
				set[supplierKey] = struct{}{}
			}
		}
	}
	if len(index) == 0 {
		return nil
	}
	return index
}

func scopeFromAccessMetadata(c *gin.Context) requestModelScope {
	if c == nil {
		return requestModelScope{}
	}
	value, exists := c.Get("accessMetadata")
	if !exists {
		return requestModelScope{}
	}

	var suppliersRaw string
	var modelsRaw string
	switch typed := value.(type) {
	case map[string]string:
		suppliersRaw = typed[apikeyscope.ScopeAllowedSuppliersMetadataKey]
		modelsRaw = typed[apikeyscope.ScopeAllowedModelsMetadataKey]
	case map[string]any:
		if v, ok := typed[apikeyscope.ScopeAllowedSuppliersMetadataKey].(string); ok {
			suppliersRaw = v
		}
		if v, ok := typed[apikeyscope.ScopeAllowedModelsMetadataKey].(string); ok {
			modelsRaw = v
		}
	default:
		return requestModelScope{}
	}

	return requestModelScope{
		allowedSuppliers: toSet(apikeyscope.DecodeScopeValues(suppliersRaw)),
		allowedModels:    toSet(apikeyscope.NormalizeModelKeys(apikeyscope.DecodeScopeValues(modelsRaw))),
	}
}

func (s requestModelScope) matchesModel(modelKeys []string) bool {
	if len(s.allowedModels) == 0 {
		return true
	}
	for _, key := range modelKeys {
		if _, ok := s.allowedModels[key]; ok {
			return true
		}
	}
	return false
}

func (s requestModelScope) matchesSupplier(modelKeys []string, modelSuppliers map[string]map[string]struct{}) bool {
	if len(s.allowedSuppliers) == 0 {
		return true
	}
	for _, key := range modelKeys {
		suppliers := modelSuppliers[key]
		if len(suppliers) == 0 {
			continue
		}
		for supplier := range suppliers {
			if _, ok := s.allowedSuppliers[supplier]; ok {
				return true
			}
		}
	}
	return false
}

func toSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out[trimmed] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func modelComparableKeys(model map[string]any) []string {
	if len(model) == 0 {
		return nil
	}
	id, _ := model["id"].(string)
	name, _ := model["name"].(string)
	return modelKeysFromInfo(id, name)
}

func modelKeysFromInfo(id, name string) []string {
	out := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	appendKey := func(raw string) {
		key := apikeyscope.NormalizeModelKey(raw)
		if key == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	appendKey(id)
	appendKey(name)
	if len(out) == 0 {
		return nil
	}
	return out
}
