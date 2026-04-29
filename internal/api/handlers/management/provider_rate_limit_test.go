package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestProviderRateLimit_Get(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		ProviderRateLimit: config.ProviderRateLimitConfig{
			Enabled:                 boolPtr(false),
			Scope:                   config.ProviderRateLimitScopeProviderModel,
			RateLimit:               12,
			RateWindowSeconds:       30,
			MaxStreamConcurrency:    2,
			ReactiveBaseDelayMS:     200,
			ReactiveMaxDelaySeconds: 8,
			ReactiveJitterMS:        50,
		},
	}
	h, _ := newReasoningDefaultsTestHandler(t, cfg)
	router := gin.New()
	router.GET("/v0/management/provider-rate-limit", h.GetProviderRateLimit)

	req := httptest.NewRequest(http.MethodGet, "/v0/management/provider-rate-limit", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload struct {
		Value config.ProviderRateLimitConfig `json:"provider-rate-limit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Value.Scope != config.ProviderRateLimitScopeProviderModel {
		t.Fatalf("scope = %q, want %q", payload.Value.Scope, config.ProviderRateLimitScopeProviderModel)
	}
	if payload.Value.RateLimit != 12 {
		t.Fatalf("rate-limit = %d, want 12", payload.Value.RateLimit)
	}
}

func TestProviderRateLimit_Put(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	h, configPath := newReasoningDefaultsTestHandler(t, cfg)
	router := gin.New()
	router.PUT("/v0/management/provider-rate-limit", h.PutProviderRateLimit)

	req := httptest.NewRequest(
		http.MethodPut,
		"/v0/management/provider-rate-limit",
		bytes.NewBufferString(`{"value":{"enabled":false,"scope":"provider-model","rate-limit":55,"rate-window-seconds":45,"max-stream-concurrency":3,"reactive-base-delay-ms":1500,"reactive-max-delay-seconds":90,"reactive-jitter-ms":100,"adaptive-enabled":true,"adaptive-increase-on-success":false,"adaptive-decrease-factor":0.7,"adaptive-min-rate-limit":2}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if cfg.ProviderRateLimit.EnabledOrDefault() {
		t.Fatal("enabled should be false")
	}
	if cfg.ProviderRateLimit.Scope != config.ProviderRateLimitScopeProviderModel {
		t.Fatalf("scope = %q, want %q", cfg.ProviderRateLimit.Scope, config.ProviderRateLimitScopeProviderModel)
	}
	if cfg.ProviderRateLimit.RateLimit != 55 {
		t.Fatalf("rate-limit = %d, want 55", cfg.ProviderRateLimit.RateLimit)
	}
	if cfg.ProviderRateLimit.AdaptiveDecreaseFactor != 0.7 {
		t.Fatalf("adaptive-decrease-factor = %v, want 0.7", cfg.ProviderRateLimit.AdaptiveDecreaseFactor)
	}

	persisted, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}
	if !bytes.Contains(persisted, []byte("provider-rate-limit")) {
		t.Fatalf("persisted config missing provider-rate-limit key: %s", string(persisted))
	}
}

func TestProviderRateLimit_PutInvalidScope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	h, _ := newReasoningDefaultsTestHandler(t, cfg)
	router := gin.New()
	router.PUT("/v0/management/provider-rate-limit", h.PutProviderRateLimit)

	req := httptest.NewRequest(
		http.MethodPut,
		"/v0/management/provider-rate-limit",
		bytes.NewBufferString(`{"value":{"scope":"invalid"}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestProviderRateLimit_GetOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newReasoningDefaultsTestHandler(t, &config.Config{})
	router := gin.New()
	router.GET("/v0/management/provider-rate-limit/options", h.GetProviderRateLimitOptions)

	req := httptest.NewRequest(http.MethodGet, "/v0/management/provider-rate-limit/options", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := payload["providers"]; !ok {
		t.Fatalf("missing providers in payload: %s", rec.Body.String())
	}
	if _, ok := payload["models"]; !ok {
		t.Fatalf("missing models in payload: %s", rec.Body.String())
	}
}

func boolPtr(v bool) *bool {
	return &v
}
