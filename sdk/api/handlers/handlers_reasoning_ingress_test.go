package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	log "github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	"github.com/tidwall/gjson"
)

func ingressTestContext(method, path string) context.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(method, path, nil)
	return context.WithValue(context.Background(), "gin", ginCtx)
}

func mustApplyIngressReasoningDefaults(t *testing.T, ctx context.Context, cfg *sdkconfig.SDKConfig, handlerType string, rawJSON []byte) ([]byte, bool) {
	t.Helper()
	out, changed, _, err := applyIngressReasoningDefaults(ctx, cfg, handlerType, rawJSON)
	if err != nil {
		t.Fatalf("applyIngressReasoningDefaults() unexpected error: %v", err)
	}
	return out, changed
}

func TestApplyIngressReasoningDefaults_OpenAIMissingOnlyInject(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{
		DefaultReasoningOnIngressByFormat: map[string]internalconfig.ReasoningIngressDefault{
			internalconfig.ReasoningIngressFormatOpenAI: {
				Policy: internalconfig.ReasoningIngressPolicyMissingOnly,
				Mode:   internalconfig.ReasoningModeEffort,
				Value:  "xhigh",
			},
		},
	}

	ctx := ingressTestContext("POST", "/v1/chat/completions")
	body := []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hello"}]}`)

	out, changed := mustApplyIngressReasoningDefaults(t, ctx, cfg, "openai", body)
	if !changed {
		t.Fatalf("expected ingress reasoning defaults to be applied")
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "xhigh" {
		t.Fatalf("reasoning_effort = %q, want %q", got, "xhigh")
	}
	if gjson.GetBytes(out, "reasoning.effort").Exists() {
		t.Fatal("reasoning.effort should not be injected for chat completions")
	}
}

func TestApplyIngressReasoningDefaults_OpenAIResponsesNormalizesLegacyEffort(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
		t.Run(path, func(t *testing.T) {
			out, changed := mustApplyIngressReasoningDefaults(t,
				ingressTestContext("POST", path),
				nil,
				"openai-response",
				[]byte(`{"model":"gpt-5","reasoning_effort":"high"}`),
			)
			if !changed {
				t.Fatal("expected legacy reasoning_effort to be normalized")
			}
			if got := gjson.GetBytes(out, "reasoning.effort").String(); got != "high" {
				t.Fatalf("reasoning.effort = %q, want high", got)
			}
			if gjson.GetBytes(out, "reasoning_effort").Exists() {
				t.Fatal("reasoning_effort should be removed for Responses")
			}
		})
	}
}

func TestIngressReasoningCompatibilityEventsArePayloadFree(t *testing.T) {
	hook := logrustest.NewLocal(log.StandardLogger())
	defer hook.Reset()

	ctx := logging.WithRequestID(ingressTestContext("POST", "/v1/responses"), "request-compatibility")
	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil)
	_, _, _ = handler.ExecuteWithAuthManager(
		ctx,
		"openai-response",
		"unknown-model(xhigh)",
		[]byte(`{"model":"unknown-model(xhigh)","input":"prompt-secret","authorization":"Bearer token-secret","reasoning_effort":"xhigh"}`),
		"",
	)
	assertReasoningCompatibilityEvent(t, hook, "accepted", "legacy_alias_normalized", log.InfoLevel)

	hook.Reset()
	_, _, errMsg := handler.ExecuteWithAuthManager(
		ctx,
		"openai-response",
		"unknown-model(xhigh)",
		[]byte(`{"model":"unknown-model(xhigh)","input":"prompt-secret","authorization":"Bearer token-secret","reasoning_effort":"low","reasoning":{"effort":"high"}}`),
		"",
	)
	assertIngressAliasConflictError(t, errMsg)
	assertReasoningCompatibilityEvent(t, hook, "rejected", "conflicting_aliases", log.WarnLevel)
}

func assertReasoningCompatibilityEvent(t *testing.T, hook *logrustest.Hook, action, reason string, level log.Level) {
	t.Helper()
	for _, entry := range hook.AllEntries() {
		if entry.Message != "reasoning compatibility event" || entry.Data["action"] != action || entry.Data["reason"] != reason {
			continue
		}
		if entry.Level != level {
			t.Fatalf("event level = %s, want %s", entry.Level, level)
		}
		want := map[string]string{
			"request_id": "request-compatibility",
			"endpoint":   "/v1/responses",
			"protocol":   "http",
			"provider":   "unresolved",
			"model":      "unknown-model",
			"action":     action,
			"reason":     reason,
		}
		if len(entry.Data) != len(want) {
			t.Fatalf("event fields = %#v, want only %#v", entry.Data, want)
		}
		for key, value := range want {
			if got, ok := entry.Data[key]; !ok || got != value {
				t.Fatalf("event field %q = %#v, want %q", key, got, value)
			}
		}
		serialized := entry.Message + fmt.Sprint(entry.Data)
		for _, sensitive := range []string{"prompt-secret", "token-secret", "Bearer", "xhigh", "low", "high", "reasoning_effort"} {
			if strings.Contains(serialized, sensitive) {
				t.Fatalf("compatibility event leaked %q: %s", sensitive, serialized)
			}
		}
		return
	}
	t.Fatalf("missing reasoning compatibility event: action=%q reason=%q", action, reason)
}

func TestApplyIngressReasoningDefaults_OpenAIResponsesMatchingAliasesNormalize(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{
		DefaultReasoningOnIngressByFormat: map[string]internalconfig.ReasoningIngressDefault{
			internalconfig.ReasoningIngressFormatOpenAI: {
				Policy: internalconfig.ReasoningIngressPolicyMissingOnly,
				Mode:   internalconfig.ReasoningModeEffort,
				Value:  "xhigh",
			},
		},
	}
	body := []byte(`{"model":"gpt-5","reasoning_effort":" HIGH ","reasoning":{"effort":"high"}}`)

	out, changed := mustApplyIngressReasoningDefaults(t, ingressTestContext("POST", "/v1/responses"), cfg, "openai-response", body)
	if !changed {
		t.Fatal("expected matching aliases to be normalized")
	}
	if got := gjson.GetBytes(out, "reasoning.effort").String(); got != "high" {
		t.Fatalf("reasoning.effort = %q, want high", got)
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Fatal("reasoning_effort should be removed for matching aliases")
	}
}

func TestApplyIngressReasoningDefaults_OpenAIResponsesRejectsConflictingAliases(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{
		DefaultReasoningOnIngressByFormat: map[string]internalconfig.ReasoningIngressDefault{
			internalconfig.ReasoningIngressFormatOpenAI: {
				Policy: internalconfig.ReasoningIngressPolicyMissingOnly,
				Mode:   internalconfig.ReasoningModeEffort,
				Value:  "xhigh",
			},
		},
	}
	body := []byte(`{"model":"gpt-5","reasoning_effort":"low","reasoning":{"effort":"high"}}`)

	out, changed, _, err := applyIngressReasoningDefaults(ingressTestContext("POST", "/v1/responses"), cfg, "openai-response", body)
	if err == nil {
		t.Fatal("expected conflicting aliases to be rejected")
	}
	if changed {
		t.Fatal("conflicting aliases should not modify the payload")
	}
	if !bytes.Equal(out, body) {
		t.Fatalf("conflicting payload changed unexpectedly: %s", string(out))
	}
	if got := gjson.GetBytes(BuildErrorResponseBody(400, err.Error()), "error.type").String(); got != "invalid_request_error" {
		t.Fatalf("local error type = %q, want invalid_request_error", got)
	}
}

func TestApplyIngressReasoningDefaults_OpenAIResponsesLeavesUnknownLegacyValueForProvider(t *testing.T) {
	body := []byte(`{"model":"gpt-5","reasoning_effort":"experimental"}`)

	out, changed := mustApplyIngressReasoningDefaults(t, ingressTestContext("POST", "/v1/responses"), nil, "openai-response", body)
	if !changed {
		t.Fatal("expected legacy alias to be normalized")
	}
	if got := gjson.GetBytes(out, "reasoning.effort").String(); got != "experimental" {
		t.Fatalf("reasoning.effort = %q, want experimental", got)
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Fatal("reasoning_effort should be removed for Responses")
	}
}

func TestApplyIngressReasoningDefaults_OpenAIResponsesInjectsNestedDefault(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{
		DefaultReasoningOnIngressByFormat: map[string]internalconfig.ReasoningIngressDefault{
			internalconfig.ReasoningIngressFormatOpenAI: {
				Policy: internalconfig.ReasoningIngressPolicyMissingOnly,
				Mode:   internalconfig.ReasoningModeEffort,
				Value:  "xhigh",
			},
		},
	}

	out, changed := mustApplyIngressReasoningDefaults(t,
		ingressTestContext("POST", "/v1/responses"),
		cfg,
		"openai-response",
		[]byte(`{"model":"gpt-5","input":"hello"}`),
	)
	if !changed {
		t.Fatal("expected Responses default reasoning to be applied")
	}
	if got := gjson.GetBytes(out, "reasoning.effort").String(); got != "xhigh" {
		t.Fatalf("reasoning.effort = %q, want xhigh", got)
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Fatal("reasoning_effort should not be injected for Responses")
	}
}

func TestApplyIngressReasoningDefaults_OpenAIMissingOnlyKeepExplicit(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{
		DefaultReasoningOnIngressByFormat: map[string]internalconfig.ReasoningIngressDefault{
			internalconfig.ReasoningIngressFormatOpenAI: {
				Policy: internalconfig.ReasoningIngressPolicyMissingOnly,
				Mode:   internalconfig.ReasoningModeEffort,
				Value:  "xhigh",
			},
		},
	}

	ctx := ingressTestContext("POST", "/v1/chat/completions")
	body := []byte(`{"model":"gpt-5","reasoning_effort":"low","reasoning":{"effort":"low"}}`)

	out, changed := mustApplyIngressReasoningDefaults(t, ctx, cfg, "openai", body)
	if changed {
		t.Fatalf("expected ingress defaults not to override explicit request values")
	}
	if !bytes.Equal(out, body) {
		t.Fatalf("payload changed unexpectedly: %s", string(out))
	}
}

func TestApplyIngressReasoningDefaults_OpenAIForceOverride(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{
		DefaultReasoningOnIngressByFormat: map[string]internalconfig.ReasoningIngressDefault{
			internalconfig.ReasoningIngressFormatOpenAI: {
				Policy: internalconfig.ReasoningIngressPolicyForceOverride,
				Mode:   internalconfig.ReasoningModeEffort,
				Value:  "high",
			},
		},
	}

	ctx := ingressTestContext("POST", "/v1/responses")
	body := []byte(`{"model":"gpt-5","reasoning_effort":"none","reasoning":{"effort":"low"}}`)

	out, changed := mustApplyIngressReasoningDefaults(t, ctx, cfg, "openai-response", body)
	if !changed {
		t.Fatalf("expected ingress defaults to override explicit request values")
	}
	if got := gjson.GetBytes(out, "reasoning.effort").String(); got != "high" {
		t.Fatalf("reasoning.effort = %q, want %q", got, "high")
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Fatal("reasoning_effort should not be injected for Responses")
	}
}

func TestApplyIngressReasoningDefaults_RejectsConflictingAliasesBeforeProviderSelection(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{
		DefaultReasoningOnIngressByFormat: map[string]internalconfig.ReasoningIngressDefault{
			internalconfig.ReasoningIngressFormatOpenAI: {
				Policy: internalconfig.ReasoningIngressPolicyMissingOnly,
				Mode:   internalconfig.ReasoningModeEffort,
				Value:  "xhigh",
			},
		},
	}
	handler := NewBaseAPIHandlers(cfg, nil)
	ctx := ingressTestContext("POST", "/v1/responses")
	body := []byte(`{"model":"unknown-model","reasoning_effort":"low","reasoning":{"effort":"high"}}`)

	_, _, errMsg := handler.ExecuteWithAuthManager(ctx, "openai-response", "unknown-model", body, "")
	assertIngressAliasConflictError(t, errMsg)
	_, _, errMsg = handler.ExecuteCountWithAuthManager(ctx, "openai-response", "unknown-model", body, "")
	assertIngressAliasConflictError(t, errMsg)

	data, headers, errChan := handler.ExecuteStreamWithAuthManager(ctx, "openai-response", "unknown-model", body, "")
	if data != nil || headers != nil {
		t.Fatalf("stream conflict should not initialize an upstream request: data=%v headers=%v", data, headers)
	}
	assertIngressAliasConflictError(t, <-errChan)
	if _, ok := <-errChan; ok {
		t.Fatal("stream error channel should close after the local conflict error")
	}
}

func assertIngressAliasConflictError(t *testing.T, errMsg *interfaces.ErrorMessage) {
	t.Helper()
	if errMsg == nil {
		t.Fatal("expected local alias conflict error")
	}
	if errMsg.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", errMsg.StatusCode, http.StatusBadRequest)
	}
	if got := gjson.GetBytes(BuildErrorResponseBody(errMsg.StatusCode, errMsg.Error.Error()), "error.type").String(); got != "invalid_request_error" {
		t.Fatalf("local error type = %q, want invalid_request_error", got)
	}
}

func TestApplyIngressReasoningDefaults_ClaudeAdaptive(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{
		DefaultReasoningOnIngressByFormat: map[string]internalconfig.ReasoningIngressDefault{
			internalconfig.ReasoningIngressFormatClaude: {
				Policy: internalconfig.ReasoningIngressPolicyMissingOnly,
				Mode:   internalconfig.ReasoningModeAdaptiveEffort,
				Value:  "high",
			},
		},
	}

	ctx := ingressTestContext("POST", "/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-5","thinking":{"budget_tokens":1024}}`)

	out, changed := mustApplyIngressReasoningDefaults(t, ctx, cfg, "claude", body)
	if !changed {
		t.Fatalf("expected ingress defaults to be applied")
	}
	if got := gjson.GetBytes(out, "thinking.type").String(); got != "adaptive" {
		t.Fatalf("thinking.type = %q, want %q", got, "adaptive")
	}
	if got := gjson.GetBytes(out, "output_config.effort").String(); got != "high" {
		t.Fatalf("output_config.effort = %q, want %q", got, "high")
	}
	if gjson.GetBytes(out, "thinking.budget_tokens").Exists() {
		t.Fatalf("thinking.budget_tokens should be removed")
	}
}

func TestApplyIngressReasoningDefaults_ClaudeDisabledForceOverride(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{
		DefaultReasoningOnIngressByFormat: map[string]internalconfig.ReasoningIngressDefault{
			internalconfig.ReasoningIngressFormatClaude: {
				Policy: internalconfig.ReasoningIngressPolicyForceOverride,
				Mode:   internalconfig.ReasoningModeDisabled,
				Value:  "disabled",
			},
		},
	}

	ctx := ingressTestContext("POST", "/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-5","thinking":{"type":"adaptive","budget_tokens":1024},"output_config":{"effort":"high"}}`)

	out, changed := mustApplyIngressReasoningDefaults(t, ctx, cfg, "claude", body)
	if !changed {
		t.Fatalf("expected ingress defaults to be applied")
	}
	if got := gjson.GetBytes(out, "thinking.type").String(); got != "disabled" {
		t.Fatalf("thinking.type = %q, want %q", got, "disabled")
	}
	if gjson.GetBytes(out, "output_config.effort").Exists() {
		t.Fatalf("output_config.effort should be removed")
	}
	if gjson.GetBytes(out, "thinking.budget_tokens").Exists() {
		t.Fatalf("thinking.budget_tokens should be removed")
	}
}

func TestApplyIngressReasoningDefaults_GeminiLevel(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{
		DefaultReasoningOnIngressByFormat: map[string]internalconfig.ReasoningIngressDefault{
			internalconfig.ReasoningIngressFormatGemini: {
				Policy: internalconfig.ReasoningIngressPolicyMissingOnly,
				Mode:   internalconfig.ReasoningModeLevel,
				Value:  "medium",
			},
		},
	}

	ctx := ingressTestContext("POST", "/v1beta/models/gemini-2.5-pro:streamGenerateContent")
	body := []byte(`{"generationConfig":{"thinkingConfig":{"thinkingLevel":"","thinkingBudget":1024,"thinking_budget":2048}}}`)

	out, changed := mustApplyIngressReasoningDefaults(t, ctx, cfg, "gemini", body)
	if !changed {
		t.Fatalf("expected ingress defaults to be applied")
	}
	if got := gjson.GetBytes(out, "generationConfig.thinkingConfig.thinkingLevel").String(); got != "medium" {
		t.Fatalf("thinkingLevel = %q, want %q", got, "medium")
	}
	if gjson.GetBytes(out, "generationConfig.thinkingConfig.thinkingBudget").Exists() {
		t.Fatalf("thinkingBudget should be removed")
	}
	if gjson.GetBytes(out, "generationConfig.thinkingConfig.thinking_budget").Exists() {
		t.Fatalf("thinking_budget should be removed")
	}
}

func TestApplyIngressReasoningDefaults_ScopeNotMatched(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{
		DefaultReasoningOnIngressByFormat: map[string]internalconfig.ReasoningIngressDefault{
			internalconfig.ReasoningIngressFormatOpenAI: {
				Policy: internalconfig.ReasoningIngressPolicyMissingOnly,
				Mode:   internalconfig.ReasoningModeEffort,
				Value:  "xhigh",
			},
		},
	}

	ctx := ingressTestContext("POST", "/v1/completions")
	body := []byte(`{"model":"gpt-5","prompt":"hello"}`)

	out, changed := mustApplyIngressReasoningDefaults(t, ctx, cfg, "openai", body)
	if changed {
		t.Fatalf("expected ingress defaults not to be applied for unmatched route")
	}
	if !bytes.Equal(out, body) {
		t.Fatalf("payload changed unexpectedly: %s", string(out))
	}
}
