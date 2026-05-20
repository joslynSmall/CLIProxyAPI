package auth

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type availabilityCacheExecutor struct {
	id string

	mu       sync.Mutex
	calls    []string
	failures map[string]error
}

func (e *availabilityCacheExecutor) Identifier() string { return e.id }

func (e *availabilityCacheExecutor) Execute(_ context.Context, auth *Auth, req cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	e.recordCall(auth.ID, req.Model)
	if err := e.failure(auth.ID); err != nil {
		return cliproxyexecutor.Response{}, err
	}
	return cliproxyexecutor.Response{Payload: []byte(auth.ID)}, nil
}

func (e *availabilityCacheExecutor) ExecuteStream(_ context.Context, auth *Auth, req cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	e.recordCall(auth.ID, req.Model)
	if err := e.failure(auth.ID); err != nil {
		return nil, err
	}
	ch := make(chan cliproxyexecutor.StreamChunk, 1)
	ch <- cliproxyexecutor.StreamChunk{Payload: []byte(auth.ID)}
	close(ch)
	return &cliproxyexecutor.StreamResult{Chunks: ch}, nil
}

func (e *availabilityCacheExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) { return auth, nil }

func (e *availabilityCacheExecutor) CountTokens(_ context.Context, auth *Auth, req cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.Execute(context.Background(), auth, req, cliproxyexecutor.Options{})
}

func (e *availabilityCacheExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func (e *availabilityCacheExecutor) recordCall(authID, model string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, authID+"|"+model)
}

func (e *availabilityCacheExecutor) failure(authID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failures == nil {
		return nil
	}
	return e.failures[authID]
}

func (e *availabilityCacheExecutor) setFailure(authID string, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failures == nil {
		e.failures = make(map[string]error)
	}
	e.failures[authID] = err
}

func (e *availabilityCacheExecutor) clearFailure(authID string) {
	e.setFailure(authID, nil)
}

func (e *availabilityCacheExecutor) callsSnapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.calls))
	copy(out, e.calls)
	return out
}

func newAvailabilityCacheManager(t *testing.T) (*Manager, *availabilityCacheExecutor, string, string) {
	t.Helper()

	m := NewManager(nil, nil, nil)
	m.SetRetryConfig(0, 0, 0)

	executor := &availabilityCacheExecutor{id: "codex"}
	m.RegisterExecutor(executor)

	baseID := uuid.NewString()
	auth1 := &Auth{ID: baseID + "-auth-1", Provider: "codex"}
	auth2 := &Auth{ID: baseID + "-auth-2", Provider: "codex"}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth1.ID, "codex", []*registry.ModelInfo{{ID: "gpt-5.4-mini"}, {ID: "gpt-5.4"}})
	reg.RegisterClient(auth2.ID, "codex", []*registry.ModelInfo{{ID: "gpt-5.4-mini"}, {ID: "gpt-5.4"}})
	t.Cleanup(func() {
		reg.UnregisterClient(auth1.ID)
		reg.UnregisterClient(auth2.ID)
	})

	if _, err := m.Register(context.Background(), auth1); err != nil {
		t.Fatalf("register auth1: %v", err)
	}
	if _, err := m.Register(context.Background(), auth2); err != nil {
		t.Fatalf("register auth2: %v", err)
	}

	return m, executor, auth1.ID, auth2.ID
}

func newAvailabilityCacheOptions(model, apiKey, session string) cliproxyexecutor.Options {
	return cliproxyexecutor.Options{
		Metadata: map[string]any{
			cliproxyexecutor.IngressRequestedModelMetadataKey: model,
			cliproxyexecutor.RequestedModelMetadataKey:        model,
			cliproxyexecutor.IngressAPIKeyMetadataKey:         apiKey,
			cliproxyexecutor.SessionAffinityMetadataKey:       session,
		},
	}
}

func TestManagerExecute_AvailabilitySuppressionShortCircuitsRepeatedRequests(t *testing.T) {
	m, executor, authID1, authID2 := newAvailabilityCacheManager(t)
	executor.setFailure(authID1, &Error{Code: "auth_unavailable", Message: "no auth available"})
	executor.setFailure(authID2, &Error{Code: "auth_unavailable", Message: "no auth available"})

	req := cliproxyexecutor.Request{Model: "gpt-5.4-mini"}
	opts := newAvailabilityCacheOptions("gpt-5.4-mini", "ingress-key", "session-1")

	_, err := m.Execute(context.Background(), []string{"codex"}, req, opts)
	if err == nil {
		t.Fatal("first execute = nil error, want auth_unavailable")
	}
	if authErr, ok := err.(*Error); !ok || authErr.Code != "auth_unavailable" {
		t.Fatalf("first execute error = %v, want auth_unavailable", err)
	}
	if got := len(executor.callsSnapshot()); got != 2 {
		t.Fatalf("first execute calls = %d, want 2", got)
	}
	m.MarkResult(context.Background(), Result{AuthID: authID1, Provider: "codex", Model: "gpt-5.4-mini", Success: true})
	m.MarkResult(context.Background(), Result{AuthID: authID2, Provider: "codex", Model: "gpt-5.4-mini", Success: true})

	_, err = m.Execute(context.Background(), []string{"codex"}, req, opts)
	if err == nil {
		t.Fatal("second execute = nil error, want auth_unavailable")
	}
	if authErr, ok := err.(*Error); !ok || authErr.Code != "auth_unavailable" {
		t.Fatalf("second execute error = %v, want auth_unavailable", err)
	}
	if got := len(executor.callsSnapshot()); got != 2 {
		t.Fatalf("second execute calls = %d, want 2", got)
	}
	if hit, _ := opts.Metadata[cliproxyexecutor.AvailabilityCacheHitMetadataKey].(bool); !hit {
		t.Fatal("availability_cache_hit should be set on cache hit")
	}

	_ = authID2
}

func TestManagerExecute_AvailabilitySuppressionScopesByModelAndSession(t *testing.T) {
	m, executor, authID1, authID2 := newAvailabilityCacheManager(t)
	executor.setFailure(authID1, &Error{Code: "auth_unavailable", Message: "no auth available"})
	executor.setFailure(authID2, &Error{Code: "auth_unavailable", Message: "no auth available"})

	req := cliproxyexecutor.Request{Model: "gpt-5.4-mini"}
	baseOpts := newAvailabilityCacheOptions("gpt-5.4-mini", "ingress-key", "session-1")
	if _, err := m.Execute(context.Background(), []string{"codex"}, req, baseOpts); err == nil {
		t.Fatal("seed execute = nil error, want auth_unavailable")
	}
	m.MarkResult(context.Background(), Result{AuthID: authID1, Provider: "codex", Model: "gpt-5.4-mini", Success: true})
	m.MarkResult(context.Background(), Result{AuthID: authID2, Provider: "codex", Model: "gpt-5.4-mini", Success: true})

	differentSession := newAvailabilityCacheOptions("gpt-5.4-mini", "ingress-key", "session-2")
	if _, err := m.Execute(context.Background(), []string{"codex"}, req, differentSession); err == nil {
		t.Fatal("different-session execute = nil error, want auth_unavailable")
	}
	if got := len(executor.callsSnapshot()); got != 4 {
		t.Fatalf("different session calls = %d, want 4", got)
	}

	differentModel := newAvailabilityCacheOptions("gpt-5.4", "ingress-key", "session-1")
	if _, err := m.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: "gpt-5.4"}, differentModel); err == nil {
		t.Fatal("different-model execute = nil error, want auth_unavailable")
	}
	if got := len(executor.callsSnapshot()); got != 6 {
		t.Fatalf("different model calls = %d, want 6", got)
	}
	_ = authID1
	_ = authID2
}

func TestManagerExecute_AvailabilitySuppressionClearedBySuccessAndUpdate(t *testing.T) {
	m, executor, authID1, authID2 := newAvailabilityCacheManager(t)
	executor.setFailure(authID1, &Error{Code: "auth_unavailable", Message: "no auth available"})
	executor.setFailure(authID2, &Error{Code: "auth_unavailable", Message: "no auth available"})

	req := cliproxyexecutor.Request{Model: "gpt-5.4-mini"}
	opts := newAvailabilityCacheOptions("gpt-5.4-mini", "ingress-key", "session-1")

	if _, err := m.Execute(context.Background(), []string{"codex"}, req, opts); err == nil {
		t.Fatal("seed execute = nil error, want auth_unavailable")
	}
	m.MarkResult(context.Background(), Result{AuthID: authID1, Provider: "codex", Model: "gpt-5.4-mini", Success: true})
	m.MarkResult(context.Background(), Result{AuthID: authID2, Provider: "codex", Model: "gpt-5.4-mini", Success: true})

	if auth, ok := m.GetByID(authID1); !ok || auth == nil {
		t.Fatal("expected auth1 to exist")
	} else if _, err := m.Update(context.Background(), auth); err != nil {
		t.Fatalf("update auth: %v", err)
	}

	executor.clearFailure(authID1)
	executor.clearFailure(authID2)
	if resp, err := m.Execute(context.Background(), []string{"codex"}, req, opts); err != nil {
		t.Fatalf("success execute error = %v, want nil", err)
	} else if string(resp.Payload) == "" {
		t.Fatal("success execute returned empty payload")
	}

	executor.setFailure(authID1, &Error{Code: "auth_unavailable", Message: "no auth available"})
	executor.setFailure(authID2, &Error{Code: "auth_unavailable", Message: "no auth available"})
	if _, err := m.Execute(context.Background(), []string{"codex"}, req, opts); err == nil {
		t.Fatal("post-success execute = nil error, want auth_unavailable")
	}
	if got := len(executor.callsSnapshot()); got != 5 {
		t.Fatalf("post-success calls = %d, want 5", got)
	}

	_ = authID2
}

func TestManagerExecute_AvailabilitySuppressionClearedByConfigReload(t *testing.T) {
	m, executor, authID1, authID2 := newAvailabilityCacheManager(t)
	executor.setFailure(authID1, &Error{Code: "auth_unavailable", Message: "no auth available"})
	executor.setFailure(authID2, &Error{Code: "auth_unavailable", Message: "no auth available"})

	req := cliproxyexecutor.Request{Model: "gpt-5.4-mini"}
	opts := newAvailabilityCacheOptions("gpt-5.4-mini", "ingress-key", "session-1")

	if _, err := m.Execute(context.Background(), []string{"codex"}, req, opts); err == nil {
		t.Fatal("seed execute = nil error, want auth_unavailable")
	}
	m.MarkResult(context.Background(), Result{AuthID: authID1, Provider: "codex", Model: "gpt-5.4-mini", Success: true})
	m.MarkResult(context.Background(), Result{AuthID: authID2, Provider: "codex", Model: "gpt-5.4-mini", Success: true})

	executor.clearFailure(authID1)
	executor.clearFailure(authID2)
	m.SetConfig(&internalconfig.Config{})
	if _, err := m.Execute(context.Background(), []string{"codex"}, req, opts); err != nil {
		t.Fatalf("post-config execute error = %v, want nil", err)
	}
	if got := len(executor.callsSnapshot()); got != 3 {
		t.Fatalf("post-config calls = %d, want 3", got)
	}

	_ = authID1
	_ = authID2
}

func TestManagerExecute_AvailabilitySuppressionDoesNotCrossProviders(t *testing.T) {
	m, executor, authID1, authID2 := newAvailabilityCacheManager(t)
	executor.setFailure(authID1, &Error{Code: "auth_unavailable", Message: "no auth available"})
	executor.setFailure(authID2, &Error{Code: "auth_unavailable", Message: "no auth available"})

	req := cliproxyexecutor.Request{Model: "gpt-5.4-mini"}
	opts := newAvailabilityCacheOptions("gpt-5.4-mini", "ingress-key", "session-1")
	if _, err := m.Execute(context.Background(), []string{"codex"}, req, opts); err == nil {
		t.Fatal("seed execute = nil error, want auth_unavailable")
	}
	m.MarkResult(context.Background(), Result{AuthID: authID1, Provider: "codex", Model: "gpt-5.4-mini", Success: true})
	m.MarkResult(context.Background(), Result{AuthID: authID2, Provider: "codex", Model: "gpt-5.4-mini", Success: true})

	if _, err := m.Execute(context.Background(), []string{"codex", "unknown"}, req, newAvailabilityCacheOptions("gpt-5.4-mini", "ingress-key", "session-1")); err == nil {
		t.Fatal("different-provider-set execute = nil error, want auth_unavailable")
	}
	if got := len(executor.callsSnapshot()); got != 4 {
		t.Fatalf("different provider set calls = %d, want 4", got)
	}
	_ = authID1
	_ = authID2
}
