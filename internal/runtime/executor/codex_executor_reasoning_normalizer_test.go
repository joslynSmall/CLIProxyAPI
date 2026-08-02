package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	log "github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	"github.com/tidwall/gjson"
)

func TestNormalizeCodexReasoningEffortPayload(t *testing.T) {
	for _, tc := range []struct {
		name           string
		body           []byte
		wantNested     string
		wantLegacyGone bool
	}{
		{
			name:           "migrates legacy alias",
			body:           []byte(`{"reasoning_effort":"experimental"}`),
			wantNested:     "experimental",
			wantLegacyGone: true,
		},
		{
			name:           "preserves nested canonical value",
			body:           []byte(`{"reasoning_effort":"low","reasoning":{"effort":"high"}}`),
			wantNested:     "high",
			wantLegacyGone: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, stripped := normalizeCodexReasoningEffortPayload(tc.body)
			if !stripped {
				t.Fatal("expected legacy reasoning_effort to be stripped")
			}
			if got := gjson.GetBytes(out, "reasoning.effort").String(); got != tc.wantNested {
				t.Fatalf("reasoning.effort = %q, want %q", got, tc.wantNested)
			}
			if gjson.GetBytes(out, "reasoning_effort").Exists() != !tc.wantLegacyGone {
				t.Fatalf("reasoning_effort exists = %t, want %t", gjson.GetBytes(out, "reasoning_effort").Exists(), !tc.wantLegacyGone)
			}
		})
	}
}

func TestNormalizeAndLogCodexReasoningEffortPayload_LogsPayloadFreeCompatibilityEvent(t *testing.T) {
	hook := logrustest.NewLocal(log.StandardLogger())
	defer hook.Reset()

	ctx := logging.WithRequestID(context.Background(), "request-codex-normalizer")
	out := normalizeAndLogCodexReasoningEffortPayload(
		ctx,
		"/responses",
		"http",
		"gpt-5(xhigh)",
		[]byte(`{"input":"prompt-secret","authorization":"Bearer token-secret","reasoning_effort":"xhigh"}`),
	)
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Fatal("reasoning_effort must be stripped")
	}

	for _, entry := range hook.AllEntries() {
		if entry.Message != "reasoning compatibility event" || entry.Data["action"] != "stripped" || entry.Data["reason"] != "legacy_alias" {
			continue
		}
		want := map[string]string{
			"request_id": "request-codex-normalizer",
			"endpoint":   "/responses",
			"protocol":   "http",
			"provider":   "codex",
			"model":      "gpt-5",
			"action":     "stripped",
			"reason":     "legacy_alias",
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
		for _, sensitive := range []string{"prompt-secret", "token-secret", "Bearer", "xhigh", "reasoning_effort"} {
			if strings.Contains(serialized, sensitive) {
				t.Fatalf("compatibility event leaked %q: %s", sensitive, serialized)
			}
		}
		return
	}
	t.Fatal("expected Codex alias-stripping compatibility event")
}

func TestCodexExecutorNormalizesReasoningEffortBeforeHTTPTransport(t *testing.T) {
	flows := []struct {
		name string
		path string
		run  func(*CodexExecutor, *cliproxyauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) error
	}{
		{
			name: "non-streaming",
			path: "/responses",
			run: func(executor *CodexExecutor, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) error {
				_, err := executor.Execute(context.Background(), auth, req, opts)
				return err
			},
		},
		{
			name: "compact",
			path: "/responses/compact",
			run: func(executor *CodexExecutor, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) error {
				opts.Alt = "responses/compact"
				_, err := executor.Execute(context.Background(), auth, req, opts)
				return err
			},
		},
		{
			name: "streaming",
			path: "/responses",
			run: func(executor *CodexExecutor, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) error {
				_, err := executor.ExecuteStream(context.Background(), auth, req, opts)
				return err
			},
		},
	}

	for _, flow := range flows {
		t.Run(flow.name, func(t *testing.T) {
			var requestBody []byte
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != flow.path {
					t.Errorf("path = %q, want %q", r.URL.Path, flow.path)
				}
				var err error
				requestBody, err = io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"test rejection","type":"invalid_request_error"}}`))
			}))
			defer upstream.Close()

			executor := NewCodexExecutor(&config.Config{})
			auth := &cliproxyauth.Auth{
				ID:       "codex-normalizer-test",
				Provider: "codex",
				Attributes: map[string]string{
					"api_key":  "test-key",
					"base_url": upstream.URL,
				},
			}
			req := cliproxyexecutor.Request{
				Model:   "gpt-5",
				Payload: []byte(`{"model":"gpt-5","input":"hello","reasoning_effort":"high"}`),
			}
			opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse}

			if err := flow.run(executor, auth, req, opts); err == nil {
				t.Fatal("expected upstream rejection")
			}
			if got := gjson.GetBytes(requestBody, "reasoning.effort").String(); got != "high" {
				t.Fatalf("reasoning.effort = %q, want high", got)
			}
			if gjson.GetBytes(requestBody, "reasoning_effort").Exists() {
				t.Fatal("reasoning_effort must not reach the Codex HTTP transport")
			}
		})
	}
}
