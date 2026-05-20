package executor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestRecordAPIResponseErrorClassifiesRequestBudgetTimeout(t *testing.T) {
	ctx, ginCtx := newUpstreamLogTestContext()
	cfg := &config.Config{SDKConfig: config.SDKConfig{RequestLog: true}}

	recordAPIRequest(ctx, cfg, upstreamRequestLog{URL: "https://example.com/v1/responses", Method: http.MethodPost})
	recordAPIResponseError(ctx, cfg, errors.New("upstream_timeout: upstream request budget exceeded"))

	text := upstreamResponseLogText(t, ginCtx)
	if !strings.Contains(text, "Error Category: request_budget_exceeded") {
		t.Fatalf("response log = %q, want request_budget_exceeded classification", text)
	}
	if !strings.Contains(text, "Cancel Source: request_budget") {
		t.Fatalf("response log = %q, want request_budget cancel source", text)
	}
}

func TestRecordAPIResponseErrorClassifiesContextCanceled(t *testing.T) {
	ctx, ginCtx := newUpstreamLogTestContext()
	cfg := &config.Config{SDKConfig: config.SDKConfig{RequestLog: true}}

	recordAPIRequest(ctx, cfg, upstreamRequestLog{URL: "https://example.com/v1/responses", Method: http.MethodPost})
	recordAPIResponseError(ctx, cfg, context.Canceled)

	text := upstreamResponseLogText(t, ginCtx)
	if !strings.Contains(text, "Error Category: context_canceled") {
		t.Fatalf("response log = %q, want context_canceled classification", text)
	}
	if !strings.Contains(text, "Cancel Source: downstream_cancelled") {
		t.Fatalf("response log = %q, want downstream_cancelled cancel source", text)
	}
}

func newUpstreamLogTestContext() (context.Context, *gin.Context) {
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), "gin", ginCtx)
	return ctx, ginCtx
}

func upstreamResponseLogText(t *testing.T, ginCtx *gin.Context) string {
	t.Helper()
	v, ok := ginCtx.Get(apiResponseKey)
	if !ok {
		t.Fatalf("expected %s in gin context", apiResponseKey)
	}
	b, ok := v.([]byte)
	if !ok {
		t.Fatalf("api response type = %T, want []byte", v)
	}
	return string(b)
}
