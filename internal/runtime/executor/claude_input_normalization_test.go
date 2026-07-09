package executor

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestNormalizeClaudeMessagesFromOpenAIResponsesPromotesStringInput(t *testing.T) {
	body := []byte(`{"model":"glm-5.2","max_tokens":32000,"messages":[],"stream":false}`)
	original := []byte(`{"model":"glm-5.2","input":"say hi","stream":false}`)

	got := normalizeClaudeMessagesFromOpenAIResponses(body, original)
	messages := gjson.GetBytes(got, "messages")
	if !messages.IsArray() || len(messages.Array()) != 1 {
		t.Fatalf("messages = %s, want one user message", messages.Raw)
	}
	msg := messages.Array()[0]
	if role := msg.Get("role").String(); role != "user" {
		t.Fatalf("role = %q, want user", role)
	}
	if content := msg.Get("content").String(); content != "say hi" {
		t.Fatalf("content = %q, want %q", content, "say hi")
	}
}
