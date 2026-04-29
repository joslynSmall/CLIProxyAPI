package management

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
)

func (h *Handler) GetProviderRateLimitOptions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"providers": collectProviderRateLimitProviderOptions(h),
		"models":    collectRuntimeModelOptions(),
	})
}

func collectProviderRateLimitProviderOptions(h *Handler) []string {
	if h == nil || h.authManager == nil {
		return nil
	}
	auths := h.authManager.List()
	values := make([]string, 0, len(auths))
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		provider := strings.ToLower(strings.TrimSpace(auth.Provider))
		if provider == "" && auth.Attributes != nil {
			provider = strings.ToLower(strings.TrimSpace(auth.Attributes["provider_key"]))
		}
		if provider == "" {
			continue
		}
		values = append(values, provider)
	}
	values = dedupeStrings(values)
	sort.Strings(values)
	return values
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
