package management

import (
	"context"
	"errors"
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestSaveTokenRecordRunsAuthSavedHookAfterPersistence(t *testing.T) {
	store := &memoryAuthStore{}
	handler := NewHandlerWithoutConfigFilePath(nil, nil)
	handler.tokenStore = store

	record := &coreauth.Auth{ID: "codex-test.json", Provider: "codex"}
	hookCalled := false
	handler.SetAuthSavedHook(func(_ context.Context, got *coreauth.Auth, savedPath string) error {
		hookCalled = true
		if got != record {
			t.Fatal("auth saved hook received a different record")
		}
		if savedPath != record.ID {
			t.Fatalf("saved path = %q, want %q", savedPath, record.ID)
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		if store.items[record.ID] == nil {
			t.Fatal("auth saved hook ran before persistence completed")
		}
		return nil
	})

	savedPath, err := handler.saveTokenRecord(context.Background(), record)
	if err != nil {
		t.Fatalf("saveTokenRecord() error = %v", err)
	}
	if savedPath != record.ID {
		t.Fatalf("saved path = %q, want %q", savedPath, record.ID)
	}
	if !hookCalled {
		t.Fatal("expected auth saved hook to run")
	}
}

func TestSaveTokenRecordReturnsPersistedPathWhenActivationFails(t *testing.T) {
	store := &memoryAuthStore{}
	handler := NewHandlerWithoutConfigFilePath(nil, nil)
	handler.tokenStore = store
	activationErr := errors.New("activation failed")
	handler.SetAuthSavedHook(func(context.Context, *coreauth.Auth, string) error {
		return activationErr
	})

	record := &coreauth.Auth{ID: "codex-test.json", Provider: "codex"}
	savedPath, err := handler.saveTokenRecord(context.Background(), record)
	if !errors.Is(err, activationErr) {
		t.Fatalf("saveTokenRecord() error = %v, want activation error", err)
	}
	if savedPath != record.ID {
		t.Fatalf("saved path = %q, want persisted path %q", savedPath, record.ID)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.items[record.ID] == nil {
		t.Fatal("auth record should remain persisted when activation fails")
	}
}
