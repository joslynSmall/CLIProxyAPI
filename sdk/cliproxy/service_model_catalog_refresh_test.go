package cliproxy

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func TestRefreshModelRegistrationsClearsExplicitlyRetiredProviders(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	service := &Service{cfg: &config.Config{}, coreManager: manager}
	auths := []*coreauth.Auth{
		{ID: "qwen-retired-test", Provider: "qwen", Status: coreauth.StatusActive},
		{ID: "iflow-retired-test", Provider: "iflow", Status: coreauth.StatusActive},
		{ID: "codex-unrelated-test", Provider: "codex", Status: coreauth.StatusActive},
	}
	for _, auth := range auths {
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register %s: %v", auth.ID, err)
		}
	}

	reg := GlobalModelRegistry()
	reg.RegisterClient(auths[0].ID, "qwen", []*registry.ModelInfo{{ID: "qwen-old"}})
	reg.RegisterClient(auths[1].ID, "iflow", []*registry.ModelInfo{{ID: "iflow-old"}})
	reg.RegisterClient(auths[2].ID, "codex", []*registry.ModelInfo{{ID: "codex-still-present"}})
	t.Cleanup(func() {
		for _, auth := range auths {
			reg.UnregisterClient(auth.ID)
		}
	})

	if refreshed := service.refreshModelRegistrations([]string{"qwen", "iflow"}); refreshed != 2 {
		t.Fatalf("refreshed auths = %d, want 2", refreshed)
	}
	registeredModels := registry.GetGlobalRegistry()
	for _, auth := range auths[:2] {
		if models := registeredModels.GetModelsForClient(auth.ID); len(models) != 0 {
			t.Fatalf("models for %s = %v, want retired registration removed", auth.ID, models)
		}
	}
	if models := registeredModels.GetModelsForClient(auths[2].ID); len(models) != 1 || models[0].ID != "codex-still-present" {
		t.Fatalf("unrelated codex models = %v, want unchanged", models)
	}
}
