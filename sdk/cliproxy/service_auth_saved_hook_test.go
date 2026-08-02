package cliproxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/watcher"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func TestActivatePersistedCodexAuthRegistersPlanModelsBeforeReturning(t *testing.T) {
	authDir := t.TempDir()
	fileName := "codex-test-plus.json"
	savedPath := filepath.Join(authDir, fileName)
	idToken := testCodexIDToken(t, "plus")
	payload, err := json.Marshal(map[string]any{
		"type":          "codex",
		"email":         "test@example.com",
		"account_id":    "account-test",
		"access_token":  "access-test",
		"refresh_token": "refresh-test",
		"id_token":      idToken,
	})
	if err != nil {
		t.Fatalf("marshal auth file: %v", err)
	}
	if err = os.WriteFile(savedPath, payload, 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	service := &Service{
		cfg:         &config.Config{AuthDir: authDir},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}
	watcherWrapper, err := defaultWatcherFactory(filepath.Join(t.TempDir(), "config.yaml"), authDir, nil, nil)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	t.Cleanup(func() { _ = watcherWrapper.Stop() })
	watcherWrapper.SetConfig(service.cfg)
	service.watcher = watcherWrapper
	t.Cleanup(func() {
		GlobalModelRegistry().UnregisterClient(fileName)
	})

	record := &coreauth.Auth{ID: fileName, Provider: "codex"}
	if err = service.activatePersistedCodexAuth(context.Background(), record, savedPath); err != nil {
		t.Fatalf("activatePersistedCodexAuth() error = %v", err)
	}

	registered, ok := service.coreManager.GetByID(fileName)
	if !ok || registered == nil {
		t.Fatal("expected persisted codex auth to be registered")
	}
	if got := registered.Attributes["plan_type"]; got != "plus" {
		t.Fatalf("plan_type = %q, want plus", got)
	}

	models := registry.GetGlobalRegistry().GetModelsForClient(fileName)
	wantModels := registry.GetCodexPlusModels()
	if len(models) != len(wantModels) {
		t.Fatalf("registered models = %d, want %d", len(models), len(wantModels))
	}
}

func TestActivatePersistedCodexAuthIgnoresOtherProviders(t *testing.T) {
	service := &Service{}
	err := service.activatePersistedCodexAuth(context.Background(), &coreauth.Auth{Provider: "claude"}, "")
	if err != nil {
		t.Fatalf("activatePersistedCodexAuth() error = %v", err)
	}
}

func TestActivatePersistedCodexAuthFallsBackWhenWatcherIsUnavailable(t *testing.T) {
	authDir := t.TempDir()
	fileName := "codex-fallback.json"
	savedPath := filepath.Join(authDir, fileName)
	payload, err := json.Marshal(map[string]any{
		"type":         "codex",
		"access_token": "access-test",
		"id_token":     testCodexIDToken(t, "free"),
	})
	if err != nil {
		t.Fatalf("marshal auth file: %v", err)
	}
	if err = os.WriteFile(savedPath, payload, 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	service := &Service{
		cfg:         &config.Config{AuthDir: authDir},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}
	t.Cleanup(func() { GlobalModelRegistry().UnregisterClient(fileName) })
	if err = service.activatePersistedCodexAuth(context.Background(), &coreauth.Auth{Provider: "codex"}, savedPath); err != nil {
		t.Fatalf("activatePersistedCodexAuth() error = %v", err)
	}
	if models := registry.GetGlobalRegistry().GetModelsForClient(fileName); len(models) != len(registry.GetCodexFreeModels()) {
		t.Fatalf("fallback registered %d models, want %d", len(models), len(registry.GetCodexFreeModels()))
	}
}

func TestHandleAuthUpdateIgnoresStaleCodexDeleteWhileFileExists(t *testing.T) {
	authDir := t.TempDir()
	fileName := "codex-stale-delete.json"
	savedPath := filepath.Join(authDir, fileName)
	if err := os.WriteFile(savedPath, []byte(`{"type":"codex"}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	service := &Service{
		cfg:         &config.Config{AuthDir: authDir},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}
	t.Cleanup(func() { GlobalModelRegistry().UnregisterClient(fileName) })
	service.applyCoreAuthAddOrUpdate(context.Background(), &coreauth.Auth{
		ID:         fileName,
		Provider:   "codex",
		Status:     coreauth.StatusActive,
		Attributes: map[string]string{"path": savedPath},
	})

	service.handleAuthUpdate(context.Background(), watcher.AuthUpdate{
		Action: watcher.AuthUpdateActionDelete,
		ID:     fileName,
	})
	active, ok := service.coreManager.GetByID(fileName)
	if !ok || active == nil || active.Disabled {
		t.Fatal("stale delete disabled a codex auth whose persisted file still exists")
	}

	if err := os.Remove(savedPath); err != nil {
		t.Fatalf("remove auth file: %v", err)
	}
	service.handleAuthUpdate(context.Background(), watcher.AuthUpdate{
		Action: watcher.AuthUpdateActionDelete,
		ID:     fileName,
	})
	removed, ok := service.coreManager.GetByID(fileName)
	if !ok || removed == nil || !removed.Disabled {
		t.Fatal("delete should disable codex auth after its persisted file is removed")
	}
}

func TestHandleAuthUpdateRejectsOlderCodexFileSnapshot(t *testing.T) {
	authDir := t.TempDir()
	fileName := "codex-concurrent.json"
	savedPath := filepath.Join(authDir, fileName)
	writeAuth := func(accessToken string) {
		t.Helper()
		payload, err := json.Marshal(map[string]any{
			"type":         "codex",
			"access_token": accessToken,
			"id_token":     testCodexIDToken(t, "plus"),
		})
		if err != nil {
			t.Fatalf("marshal auth file: %v", err)
		}
		if err = os.WriteFile(savedPath, payload, 0o600); err != nil {
			t.Fatalf("write auth file: %v", err)
		}
	}

	service := &Service{
		cfg:         &config.Config{AuthDir: authDir},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}
	watcherWrapper, err := defaultWatcherFactory(filepath.Join(t.TempDir(), "config.yaml"), authDir, nil, nil)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	t.Cleanup(func() { _ = watcherWrapper.Stop() })
	watcherWrapper.SetConfig(service.cfg)

	writeAuth("token-a")
	authsA, err := watcherWrapper.SyncAuthFile(savedPath)
	if err != nil || len(authsA) != 1 {
		t.Fatalf("sync token A: auths=%d err=%v", len(authsA), err)
	}
	writeAuth("token-b")
	authsB, err := watcherWrapper.SyncAuthFile(savedPath)
	if err != nil || len(authsB) != 1 {
		t.Fatalf("sync token B: auths=%d err=%v", len(authsB), err)
	}

	service.handleAuthUpdate(context.Background(), watcher.AuthUpdate{Action: watcher.AuthUpdateActionModify, Auth: authsB[0]})
	service.handleAuthUpdate(context.Background(), watcher.AuthUpdate{Action: watcher.AuthUpdateActionModify, Auth: authsA[0]})

	registered, ok := service.coreManager.GetByID(fileName)
	if !ok || registered == nil {
		t.Fatal("expected current codex auth to remain registered")
	}
	if got := registered.Metadata["access_token"]; got != "token-b" {
		t.Fatalf("active access token = %v, want token-b", got)
	}
}

func testCodexIDToken(t *testing.T, planType string) string {
	t.Helper()
	claims, err := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type": planType,
		},
	})
	if err != nil {
		t.Fatalf("marshal JWT claims: %v", err)
	}
	return "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
}
