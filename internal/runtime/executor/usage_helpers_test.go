package executor

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestParseOpenAIUsageChatCompletions(t *testing.T) {
	data := []byte(`{"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":5}}}`)
	detail := parseOpenAIUsage(data)
	if detail.InputTokens != 1 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 1)
	}
	if detail.OutputTokens != 2 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 2)
	}
	if detail.TotalTokens != 3 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 3)
	}
	if detail.CachedTokens != 4 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 4)
	}
	if detail.ReasoningTokens != 5 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 5)
	}
}

func TestParseOpenAIUsageResponses(t *testing.T) {
	data := []byte(`{"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30,"input_tokens_details":{"cached_tokens":7},"output_tokens_details":{"reasoning_tokens":9}}}`)
	detail := parseOpenAIUsage(data)
	if detail.InputTokens != 10 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 10)
	}
	if detail.OutputTokens != 20 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 20)
	}
	if detail.TotalTokens != 30 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 30)
	}
	if detail.CachedTokens != 7 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 7)
	}
	if detail.ReasoningTokens != 9 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 9)
	}
}

func TestUsageReporterBuildRecordIncludesLatency(t *testing.T) {
	reporter := &usageReporter{
		provider:    "openai",
		model:       "gpt-5.4",
		requestedAt: time.Now().Add(-1500 * time.Millisecond),
	}

	record := reporter.buildRecord(context.Background(), usage.Detail{TotalTokens: 3}, false)
	if record.Latency < time.Second {
		t.Fatalf("latency = %v, want >= 1s", record.Latency)
	}
	if record.Latency > 3*time.Second {
		t.Fatalf("latency = %v, want <= 3s", record.Latency)
	}
}

func TestUsageReporterBuildRecordIncludesFailureMetadata(t *testing.T) {
	reporter := &usageReporter{
		provider:    "openai",
		model:       "gpt-5.4",
		requestedAt: time.Now().Add(-time.Second),
	}

	reporter.captureFailure(&cliproxyauth.Error{
		Code:      "auth_unavailable",
		Message:   "no auth available",
		Retryable: true,
	})

	record := reporter.buildRecord(context.Background(), usage.Detail{}, true)
	if record.FailureStage != "auth_selection" {
		t.Fatalf("failure_stage = %q, want %q", record.FailureStage, "auth_selection")
	}
	if record.ErrorCode != "auth_unavailable" {
		t.Fatalf("error_code = %q, want %q", record.ErrorCode, "auth_unavailable")
	}
	if record.ErrorMessage != "no auth available" {
		t.Fatalf("error_message = %q, want %q", record.ErrorMessage, "no auth available")
	}
	if record.StatusCode != 503 {
		t.Fatalf("status_code = %d, want 503", record.StatusCode)
	}
}

func TestUsageReporterBuildRecordIncludesAttemptSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ginCtx := &gin.Context{}
	ginCtx.Set(apiAttemptsKey, []*upstreamAttempt{
		{index: 1, upstreamRequestIDs: []string{"up-1", "up-2"}},
		{index: 2, upstreamRequestIDs: []string{"up-2", "up-3"}},
	})
	ctx := context.WithValue(context.Background(), "gin", ginCtx)

	reporter := &usageReporter{provider: "openai", model: "gpt-5.4", requestedAt: time.Now()}
	record := reporter.buildRecord(ctx, usage.Detail{}, true)

	if record.AttemptCount != 2 {
		t.Fatalf("attempt_count = %d, want 2", record.AttemptCount)
	}
	if len(record.UpstreamRequestIDs) != 3 || record.UpstreamRequestIDs[0] != "up-1" || record.UpstreamRequestIDs[1] != "up-2" || record.UpstreamRequestIDs[2] != "up-3" {
		t.Fatalf("upstream_request_ids = %#v, want [up-1 up-2 up-3]", record.UpstreamRequestIDs)
	}
}

func TestExtractUpstreamErrorCode(t *testing.T) {
	t.Run("prefer code by default", func(t *testing.T) {
		body := []byte(`{"error":{"code":"rate_limit_exceeded","status":"RESOURCE_EXHAUSTED"}}`)
		got := extractUpstreamErrorCode(body, false)
		if got != "rate_limit_exceeded" {
			t.Fatalf("error_code = %q, want %q", got, "rate_limit_exceeded")
		}
	})

	t.Run("prefer status for google style", func(t *testing.T) {
		body := []byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}`)
		got := extractUpstreamErrorCode(body, true)
		if got != "resource_exhausted" {
			t.Fatalf("error_code = %q, want %q", got, "resource_exhausted")
		}
	})

	t.Run("fallback to error type", func(t *testing.T) {
		body := []byte(`{"error":{"type":"usage_limit_reached"}}`)
		got := extractUpstreamErrorCode(body, false)
		if got != "usage_limit_reached" {
			t.Fatalf("error_code = %q, want %q", got, "usage_limit_reached")
		}
	})

	t.Run("non json returns empty", func(t *testing.T) {
		got := extractUpstreamErrorCode([]byte("not json"), false)
		if got != "" {
			t.Fatalf("error_code = %q, want empty", got)
		}
	})
}

func TestResolveErrorFieldsFromStatusErr(t *testing.T) {
	code, message, status := resolveErrorFields(statusErr{
		code:    429,
		msg:     "rate limited",
		errCode: "rate_limit_exceeded",
	})
	if code != "rate_limit_exceeded" {
		t.Fatalf("code = %q, want %q", code, "rate_limit_exceeded")
	}
	if status != 429 {
		t.Fatalf("status = %d, want 429", status)
	}
	if message != "rate limited" {
		t.Fatalf("message = %q, want %q", message, "rate limited")
	}
}

func TestResolveErrorFieldsFromWrappedStatusErr(t *testing.T) {
	wrapped := fmt.Errorf("wrapper: %w", statusErr{
		code:    502,
		msg:     "upstream bad gateway",
		errCode: "bad_gateway",
	})

	code, message, status := resolveErrorFields(wrapped)
	if code != "bad_gateway" {
		t.Fatalf("code = %q, want %q", code, "bad_gateway")
	}
	if status != 502 {
		t.Fatalf("status = %d, want 502", status)
	}
	if !strings.Contains(message, "wrapper: upstream bad gateway") {
		t.Fatalf("message = %q, want contains wrapped text", message)
	}
}
