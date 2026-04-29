package handlers

import (
	"errors"
	"net/http"
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type codedStatusError struct {
	msg    string
	code   string
	status int
}

func (e codedStatusError) Error() string {
	return e.msg
}

func (e codedStatusError) ErrorCode() string {
	return e.code
}

func (e codedStatusError) StatusCode() int {
	return e.status
}

func TestResolveHandlerUsageError_UsesErrorCodeInterface(t *testing.T) {
	errInput := codedStatusError{
		msg:    "upstream timed out",
		code:   "upstream_timeout",
		status: http.StatusGatewayTimeout,
	}
	code, message, status := resolveHandlerUsageError(errInput, http.StatusInternalServerError)
	if code != "upstream_timeout" {
		t.Fatalf("code = %q, want %q", code, "upstream_timeout")
	}
	if message != "upstream timed out" {
		t.Fatalf("message = %q, want %q", message, "upstream timed out")
	}
	if status != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", status, http.StatusGatewayTimeout)
	}
}

func TestResolveHandlerUsageError_ExtractsCodeFromJSONErrorPayload(t *testing.T) {
	errInput := errors.New(`{"error":{"status":"RESOURCE_EXHAUSTED","code":"rate_limit_exceeded","message":"quota exceeded"}}`)
	code, message, status := resolveHandlerUsageError(errInput, http.StatusTooManyRequests)
	if code != "resource_exhausted" {
		t.Fatalf("code = %q, want %q", code, "resource_exhausted")
	}
	if message == "" {
		t.Fatal("message should not be empty")
	}
	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", status, http.StatusTooManyRequests)
	}
}

func TestResolveHandlerFailureRecordContext_UsesSelectedAuthProvider(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{
		ID:       "auth-openai-1",
		Provider: "OpenAI",
		Status:   coreauth.StatusActive,
	}
	if _, err := manager.Register(t.Context(), auth); err != nil {
		t.Fatalf("manager.Register: %v", err)
	}

	meta := map[string]any{
		coreexecutor.SelectedAuthMetadataKey: "auth-openai-1",
	}
	provider, authID, authIndex := resolveHandlerFailureRecordContext(manager, []string{"codex", "ark"}, meta)
	if provider != "openai" {
		t.Fatalf("provider = %q, want %q", provider, "openai")
	}
	if authID != "auth-openai-1" {
		t.Fatalf("authID = %q, want %q", authID, "auth-openai-1")
	}
	if authIndex == "" {
		t.Fatal("authIndex should not be empty")
	}
}

func TestResolveHandlerFailureRecordContext_DoesNotJoinMultipleProviders(t *testing.T) {
	provider, authID, authIndex := resolveHandlerFailureRecordContext(nil, []string{"codex", "ark"}, nil)
	if provider != "" {
		t.Fatalf("provider = %q, want empty", provider)
	}
	if authID != "" {
		t.Fatalf("authID = %q, want empty", authID)
	}
	if authIndex != "" {
		t.Fatalf("authIndex = %q, want empty", authIndex)
	}
}
