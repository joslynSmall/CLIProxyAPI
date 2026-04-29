package management

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/apikeyscope"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

// GetAPIKeyEntries returns api-key-entries config.
func (h *Handler) GetAPIKeyEntries(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"api-key-entries": h.cfg.APIKeyEntries})
}

// PutAPIKeyEntries replaces api-key-entries config.
func (h *Handler) PutAPIKeyEntries(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	entries, err := parseAPIKeyEntriesPayload(data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	normalized, err := normalizeAPIKeyEntriesStrict(entries)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.cfg.APIKeyEntries = normalized
	h.persist(c)
}

// PatchAPIKeyEntries updates one api-key-entries item by index or match key.
func (h *Handler) PatchAPIKeyEntries(c *gin.Context) {
	type apiKeyEntryPatch struct {
		APIKey           *string   `json:"api-key"`
		AllowedSuppliers *[]string `json:"allowed-suppliers"`
		AllowedModels    *[]string `json:"allowed-models"`
	}
	var body struct {
		Index *int              `json:"index"`
		Match *string           `json:"match"`
		Value *apiKeyEntryPatch `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	targetIndex := -1
	if body.Index != nil && *body.Index >= 0 && *body.Index < len(h.cfg.APIKeyEntries) {
		targetIndex = *body.Index
	}
	if targetIndex == -1 && body.Match != nil {
		match := strings.TrimSpace(*body.Match)
		for i := range h.cfg.APIKeyEntries {
			if h.cfg.APIKeyEntries[i].APIKey == match {
				targetIndex = i
				break
			}
		}
	}
	if targetIndex == -1 {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return
	}

	entry := h.cfg.APIKeyEntries[targetIndex]
	if body.Value.APIKey != nil {
		entry.APIKey = strings.TrimSpace(*body.Value.APIKey)
		if entry.APIKey == "" {
			h.cfg.APIKeyEntries = append(h.cfg.APIKeyEntries[:targetIndex], h.cfg.APIKeyEntries[targetIndex+1:]...)
			h.cfg.APIKeyEntries = config.NormalizeAPIKeyEntries(h.cfg.APIKeyEntries)
			h.persist(c)
			return
		}
	}
	if body.Value.AllowedSuppliers != nil {
		suppliers, err := apikeyscope.NormalizeSupplierKeys(*body.Value.AllowedSuppliers)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		entry.AllowedSuppliers = suppliers
	}
	if body.Value.AllowedModels != nil {
		entry.AllowedModels = apikeyscope.NormalizeModelKeys(*body.Value.AllowedModels)
	}

	h.cfg.APIKeyEntries[targetIndex] = entry
	normalized, err := normalizeAPIKeyEntriesStrict(h.cfg.APIKeyEntries)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.cfg.APIKeyEntries = normalized
	h.persist(c)
}

// DeleteAPIKeyEntries removes one item by api-key or index.
func (h *Handler) DeleteAPIKeyEntries(c *gin.Context) {
	if val := strings.TrimSpace(c.Query("api-key")); val != "" {
		out := make([]config.APIKeyEntry, 0, len(h.cfg.APIKeyEntries))
		for _, entry := range h.cfg.APIKeyEntries {
			if entry.APIKey != val {
				out = append(out, entry)
			}
		}
		if len(out) == len(h.cfg.APIKeyEntries) {
			c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
			return
		}
		h.cfg.APIKeyEntries = config.NormalizeAPIKeyEntries(out)
		h.persist(c)
		return
	}

	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil && idx >= 0 && idx < len(h.cfg.APIKeyEntries) {
			h.cfg.APIKeyEntries = append(h.cfg.APIKeyEntries[:idx], h.cfg.APIKeyEntries[idx+1:]...)
			h.cfg.APIKeyEntries = config.NormalizeAPIKeyEntries(h.cfg.APIKeyEntries)
			h.persist(c)
			return
		}
	}

	c.JSON(http.StatusBadRequest, gin.H{"error": "missing api-key or index"})
}

// GetAPIKeyEntriesOptions returns selectable suppliers/models for UI forms.
func (h *Handler) GetAPIKeyEntriesOptions(c *gin.Context) {
	var auths []*coreauth.Auth
	if h != nil && h.authManager != nil {
		auths = h.authManager.List()
	}
	suppliers := collectSupplierOptions(h.cfg, auths)
	models := collectRuntimeModelOptions()
	c.JSON(http.StatusOK, gin.H{
		"suppliers": suppliers,
		"models":    models,
	})
}

func parseAPIKeyEntriesPayload(data []byte) ([]config.APIKeyEntry, error) {
	var entries []config.APIKeyEntry
	if err := json.Unmarshal(data, &entries); err == nil {
		return entries, nil
	}

	var obj struct {
		Items []config.APIKeyEntry `json:"items"`
	}
	if err := json.Unmarshal(data, &obj); err != nil || len(obj.Items) == 0 {
		return nil, fmt.Errorf("invalid body")
	}
	return obj.Items, nil
}

func normalizeAPIKeyEntriesStrict(entries []config.APIKeyEntry) ([]config.APIKeyEntry, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	normalized := make([]config.APIKeyEntry, 0, len(entries))
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

		suppliers, err := apikeyscope.NormalizeSupplierKeys(raw.AllowedSuppliers)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, config.APIKeyEntry{
			APIKey:           key,
			AllowedSuppliers: suppliers,
			AllowedModels:    apikeyscope.NormalizeModelKeys(raw.AllowedModels),
		})
	}
	if len(normalized) == 0 {
		return nil, nil
	}
	return normalized, nil
}

func collectSupplierOptions(cfg *config.Config, auths []*coreauth.Auth) []string {
	values := make([]string, 0)
	if cfg != nil {
		values = make([]string, 0, len(cfg.OpenAICompatibility)+len(cfg.ClaudeKey)+len(cfg.CodexKey)+len(auths))
		for i := range cfg.OpenAICompatibility {
			values = append(values, apikeyscope.SupplierKeyForOpenAICompatibility(cfg.OpenAICompatibility[i].Name))
		}
		for i := range cfg.ClaudeKey {
			values = append(values, apikeyscope.SupplierKeyForClaudeBaseURL(cfg.ClaudeKey[i].BaseURL))
		}
		for i := range cfg.CodexKey {
			values = append(values, apikeyscope.SupplierKeyForCodexBaseURL(cfg.CodexKey[i].BaseURL))
		}
	}
	for _, auth := range auths {
		if auth == nil || auth.Attributes == nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(auth.Attributes["auth_kind"])) != "oauth" {
			continue
		}
		supplier := apikeyscope.SupplierKeyFromAuth(auth)
		if supplier == "" {
			continue
		}
		values = append(values, supplier)
	}
	deduped := dedupeStrings(values)
	sort.Strings(deduped)
	return deduped
}

func collectRuntimeModelOptions() []string {
	models := registry.GetGlobalRegistry().GetAvailableModels("")
	values := make([]string, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		id, _ := model["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		values = append(values, id)
	}
	values = dedupeStrings(values)
	sort.Strings(values)
	return values
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
