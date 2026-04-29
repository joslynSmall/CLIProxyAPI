package handlers

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/apikeyscope"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestFilterModelsByAccessScope_ModelOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("accessMetadata", map[string]string{
		apikeyscope.ScopeAllowedModelsMetadataKey: "gpt-4o-mini",
	})

	h := &BaseAPIHandler{}
	models := []map[string]any{
		{"id": "gpt-4o-mini", "object": "model"},
		{"id": "gpt-5", "object": "model"},
	}

	filtered := h.FilterModelsByAccessScope(ctx, models)
	if len(filtered) != 1 {
		t.Fatalf("filtered len = %d, want 1", len(filtered))
	}
	if filtered[0]["id"] != "gpt-4o-mini" {
		t.Fatalf("filtered[0].id = %v, want gpt-4o-mini", filtered[0]["id"])
	}
}

func TestFilterModelsByAccessScope_SupplierOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("accessMetadata", map[string]string{
		apikeyscope.ScopeAllowedSuppliersMetadataKey: "claude-api-key:https://api.anthropic.com",
	})

	manager := coreauth.NewManager(nil, nil, nil)
	_, err := manager.Register(context.Background(), &coreauth.Auth{
		ID:       "auth-claude",
		Provider: "claude",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"source":   "config:claude[token]",
			"base_url": "https://api.anthropic.com/",
		},
	})
	if err != nil {
		t.Fatalf("Register() err = %v", err)
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient("auth-claude", "claude", []*registry.ModelInfo{
		{ID: "claude-sonnet-4-20250514"},
	})
	defer reg.UnregisterClient("auth-claude")

	h := &BaseAPIHandler{AuthManager: manager}
	models := []map[string]any{
		{"id": "claude-sonnet-4-20250514", "object": "model"},
		{"id": "gpt-5.3-codex", "object": "model"},
	}

	filtered := h.FilterModelsByAccessScope(ctx, models)
	if len(filtered) != 1 {
		t.Fatalf("filtered len = %d, want 1", len(filtered))
	}
	if filtered[0]["id"] != "claude-sonnet-4-20250514" {
		t.Fatalf("filtered[0].id = %v, want claude-sonnet-4-20250514", filtered[0]["id"])
	}
}

func TestFilterModelsByAccessScope_OAuthSupplier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("accessMetadata", map[string]string{
		apikeyscope.ScopeAllowedSuppliersMetadataKey: "oauth:gemini-cli",
	})

	manager := coreauth.NewManager(nil, nil, nil)
	_, err := manager.Register(context.Background(), &coreauth.Auth{
		ID:       "auth-gemini-oauth",
		Provider: "gemini-cli",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"auth_kind": "oauth",
			"source":    "/tmp/auths/gemini.json",
		},
		Metadata: map[string]any{
			"email": "user@example.com",
		},
	})
	if err != nil {
		t.Fatalf("Register() err = %v", err)
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient("auth-gemini-oauth", "gemini-cli", []*registry.ModelInfo{
		{ID: "gemini-2.5-pro"},
	})
	defer reg.UnregisterClient("auth-gemini-oauth")

	h := &BaseAPIHandler{AuthManager: manager}
	models := []map[string]any{
		{"id": "gemini-2.5-pro", "object": "model"},
		{"id": "claude-sonnet-4-20250514", "object": "model"},
	}

	filtered := h.FilterModelsByAccessScope(ctx, models)
	if len(filtered) != 1 {
		t.Fatalf("filtered len = %d, want 1", len(filtered))
	}
	if filtered[0]["id"] != "gemini-2.5-pro" {
		t.Fatalf("filtered[0].id = %v, want gemini-2.5-pro", filtered[0]["id"])
	}
}
