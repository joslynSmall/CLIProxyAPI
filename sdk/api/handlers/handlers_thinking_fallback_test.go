package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	log "github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
)

type thinkingFallbackNonStreamExecutor struct {
	mu     sync.Mutex
	models []string
	calls  int
}

func (e *thinkingFallbackNonStreamExecutor) Identifier() string { return "codex" }

func (e *thinkingFallbackNonStreamExecutor) Execute(_ context.Context, _ *coreauth.Auth, req coreexecutor.Request, _ coreexecutor.Options) (coreexecutor.Response, error) {
	e.mu.Lock()
	e.calls++
	e.models = append(e.models, req.Model)
	call := e.calls
	e.mu.Unlock()

	if call == 1 {
		return coreexecutor.Response{}, &coreauth.Error{
			Code:       "invalid_request",
			Message:    `{"error":{"message":"level \"xhigh\" not supported, valid levels: high"}}`,
			Retryable:  false,
			HTTPStatus: http.StatusBadRequest,
		}
	}
	return coreexecutor.Response{Payload: []byte(fmt.Sprintf(`{"model":"%s"}`, req.Model))}, nil
}

func (e *thinkingFallbackNonStreamExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	return nil, &coreauth.Error{Code: "not_implemented", Message: "ExecuteStream not implemented", HTTPStatus: http.StatusNotImplemented}
}

func (e *thinkingFallbackNonStreamExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *thinkingFallbackNonStreamExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{Code: "not_implemented", Message: "CountTokens not implemented"}
}

func (e *thinkingFallbackNonStreamExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{Code: "not_implemented", Message: "HttpRequest not implemented", HTTPStatus: http.StatusNotImplemented}
}

func (e *thinkingFallbackNonStreamExecutor) Models() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.models))
	copy(out, e.models)
	return out
}

func TestShouldFallbackThinkingEffort(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "typed unsupported level",
			err:  thinking.NewThinkingError(thinking.ErrLevelNotSupported, "level not supported"),
			want: true,
		},
		{
			name: "typed budget out of range",
			err:  thinking.NewThinkingError(thinking.ErrBudgetOutOfRange, "budget out of range"),
			want: true,
		},
		{
			name: "upstream valid level constraint",
			err:  fmt.Errorf(`{"error":{"message":"level \"xhigh\" not supported, valid levels: high"}}`),
			want: true,
		},
		{
			name: "upstream unsupported effort value",
			err:  fmt.Errorf(`{"error":{"message":"unsupported effort value: xhigh"}}`),
			want: true,
		},
		{
			name: "unsupported parameter name",
			err:  fmt.Errorf(`{"detail":"Unsupported parameter: reasoning_effort"}`),
			want: false,
		},
		{
			name: "parameter unsupported without a value constraint",
			err:  fmt.Errorf(`{"error":{"message":"reasoning.effort is not supported for this request"}}`),
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldFallbackThinkingEffort(tc.err); got != tc.want {
				t.Fatalf("shouldFallbackThinkingEffort() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestExecuteWithAuthManager_FallbacksThinkingEffort(t *testing.T) {
	executor := &thinkingFallbackNonStreamExecutor{}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)

	auth := &coreauth.Auth{
		ID:       "auth-thinking-nonstream",
		Provider: "codex",
		Status:   coreauth.StatusActive,
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("manager.Register(auth): %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	manager.RefreshSchedulerEntry(auth.ID)
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)
	resp, _, errMsg := handler.ExecuteWithAuthManager(
		context.Background(),
		"openai",
		"test-model(xhigh)",
		[]byte(`{"model":"test-model(xhigh)","reasoning_effort":"xhigh"}`),
		"",
	)
	if errMsg != nil {
		t.Fatalf("unexpected error: %+v", errMsg)
	}
	if string(resp) != `{"model":"test-model(high)"}` {
		t.Fatalf("unexpected response payload: %s", string(resp))
	}

	models := executor.Models()
	if len(models) < 2 {
		t.Fatalf("expected at least 2 attempts, got %v", models)
	}
	if models[0] != "test-model(xhigh)" || models[1] != "test-model(high)" {
		t.Fatalf("unexpected attempt models: %v", models)
	}
}

func TestExecuteWithAuthManager_UnsupportedReasoningParameterDoesNotRetryOrOpenCircuitBreaker(t *testing.T) {
	hook := logrustest.NewLocal(log.StandardLogger())
	defer hook.Reset()

	const (
		authID  = "codex-reasoning-fallback-auth"
		modelID = "codex-reasoning-fallback-model"
	)

	var attempts int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		attempts++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"Unsupported parameter: reasoning_effort"}`))
	}))
	defer upstream.Close()

	executor := runtimeexecutor.NewCodexExecutor(&config.Config{
		CodexKey: []config.CodexKey{{
			APIKey:                         "test-key",
			BaseURL:                        upstream.URL,
			CircuitBreakerFailureThreshold: 3,
		}},
	})
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)
	auth := &coreauth.Auth{
		ID:       authID,
		Provider: "codex",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"api_key":  "test-key",
			"base_url": upstream.URL,
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("manager.Register(auth): %v", err)
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: modelID}})
	manager.RefreshSchedulerEntry(authID)
	t.Cleanup(func() {
		reg.ResetCircuitBreaker(authID, modelID)
		reg.UnregisterClient(authID)
	})

	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)
	requestCtx := logging.WithRequestID(ingressTestContext("POST", "/v1/responses"), "request-fallback-suppressed")
	_, _, errMsg := handler.ExecuteWithAuthManager(
		requestCtx,
		"openai-response",
		modelID+"(medium)",
		[]byte(`{"model":"codex-reasoning-fallback-model(medium)","input":"hi","reasoning":{"effort":"medium"}}`),
		"",
	)
	if errMsg == nil {
		t.Fatal("expected final upstream parameter error")
	}
	if errMsg.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", errMsg.StatusCode, http.StatusBadRequest)
	}
	if attempts != 1 {
		t.Fatalf("upstream attempts = %d, want 1 for unsupported parameter name", attempts)
	}
	if reg.IsCircuitOpen(authID, modelID) {
		t.Fatalf("reasoning parameter failures must not open circuit for %q", modelID)
	}

	for _, entry := range hook.AllEntries() {
		if entry.Message != "reasoning compatibility event" || entry.Data["action"] != "suppressed" || entry.Data["reason"] != "parameter_name_error" {
			continue
		}
		if entry.Level != log.WarnLevel {
			t.Fatalf("suppression level = %s, want %s", entry.Level, log.WarnLevel)
		}
		want := map[string]string{
			"request_id": "request-fallback-suppressed",
			"endpoint":   "/v1/responses",
			"protocol":   "http",
			"provider":   "codex",
			"model":      modelID,
			"action":     "suppressed",
			"reason":     "parameter_name_error",
		}
		if len(entry.Data) != len(want) {
			t.Fatalf("suppression fields = %#v, want only %#v", entry.Data, want)
		}
		for key, value := range want {
			if got, ok := entry.Data[key]; !ok || got != value {
				t.Fatalf("suppression field %q = %#v, want %q", key, got, value)
			}
		}
		if serialized := entry.Message + fmt.Sprint(entry.Data); strings.Contains(serialized, "Unsupported parameter: reasoning_effort") {
			t.Fatalf("suppression event leaked upstream error: %s", serialized)
		}
		return
	}
	t.Fatal("expected fallback suppression compatibility event")
}
