package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestCodexWebsocketsExecutorNormalizesReasoningEffortBeforeRequestCreation(t *testing.T) {
	flows := []struct {
		name    string
		cfg     *config.Config
		payload []byte
		run     func(*CodexWebsocketsExecutor, *cliproxyauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) error
	}{
		{
			name:    "non-streaming direct executor caller",
			cfg:     &config.Config{},
			payload: []byte(`{"model":"gpt-5","input":"hello","reasoning_effort":"high"}`),
			run: func(executor *CodexWebsocketsExecutor, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) error {
				_, err := executor.Execute(context.Background(), auth, req, opts)
				return err
			},
		},
		{
			name:    "streaming direct executor caller",
			cfg:     &config.Config{},
			payload: []byte(`{"model":"gpt-5","input":"hello","reasoning_effort":"high"}`),
			run: func(executor *CodexWebsocketsExecutor, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) error {
				result, err := executor.ExecuteStream(context.Background(), auth, req, opts)
				if err != nil {
					return err
				}
				for chunk := range result.Chunks {
					if chunk.Err != nil {
						return chunk.Err
					}
				}
				return nil
			},
		},
		{
			name: "non-streaming payload config rewrite",
			cfg: &config.Config{Payload: config.PayloadConfig{Override: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{Name: "gpt-5", Protocol: "codex"}},
				Params: map[string]any{"reasoning_effort": "high"},
			}}}},
			payload: []byte(`{"model":"gpt-5","input":"hello"}`),
			run: func(executor *CodexWebsocketsExecutor, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) error {
				_, err := executor.Execute(context.Background(), auth, req, opts)
				return err
			},
		},
	}

	for _, flow := range flows {
		t.Run(flow.name, func(t *testing.T) {
			received := make(chan []byte, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Errorf("upgrade websocket: %v", err)
					return
				}
				defer conn.Close()

				_, payload, err := conn.ReadMessage()
				if err != nil {
					t.Errorf("read websocket message: %v", err)
					return
				}
				received <- payload
				if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[]}}`)); err != nil {
					t.Errorf("write websocket message: %v", err)
				}
			}))
			defer upstream.Close()

			executor := NewCodexWebsocketsExecutor(flow.cfg)
			auth := &cliproxyauth.Auth{
				ID:       "codex-websocket-normalizer-test",
				Provider: "codex",
				Attributes: map[string]string{
					"api_key":  "test-key",
					"base_url": upstream.URL,
				},
			}
			req := cliproxyexecutor.Request{
				Model:   "gpt-5",
				Payload: flow.payload,
			}
			opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse}

			if err := flow.run(executor, auth, req, opts); err != nil {
				t.Fatalf("execute websocket request: %v", err)
			}
			select {
			case payload := <-received:
				if got := gjson.GetBytes(payload, "type").String(); got != "response.create" {
					t.Fatalf("type = %q, want response.create", got)
				}
				if got := gjson.GetBytes(payload, "reasoning.effort").String(); got != "high" {
					t.Fatalf("reasoning.effort = %q, want high", got)
				}
				if gjson.GetBytes(payload, "reasoning_effort").Exists() {
					t.Fatal("reasoning_effort must not reach the Codex WebSocket transport")
				}
			default:
				t.Fatal("upstream did not receive a WebSocket request")
			}
		})
	}
}
