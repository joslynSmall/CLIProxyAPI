package executor

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestNormalizeClaudeToolsFromOpenAIResponsesFlattensNamespaceTools(t *testing.T) {
	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"mcp__demo","description":"Demo namespace","input_schema":{}}]}`)
	original := []byte(`{"model":"glm-5.2","input":"hi","tools":[{"type":"namespace","name":"mcp__demo__","description":"Demo namespace","tools":[{"type":"function","name":"lookup","description":"Lookup item","parameters":{"type":"object","properties":{"id":{"type":"string"}}}}]}]}`)

	got := normalizeClaudeToolsFromOpenAIResponses(body, original)
	tools := gjson.GetBytes(got, "tools")
	if !tools.IsArray() || len(tools.Array()) != 1 {
		t.Fatalf("tools = %s, want one flattened tool", tools.Raw)
	}
	tool := tools.Array()[0]
	if name := tool.Get("name").String(); name != "mcp__demo__lookup" {
		t.Fatalf("tool name = %q, want %q", name, "mcp__demo__lookup")
	}
	if tool.Get("function").Exists() {
		t.Fatalf("tool contains OpenAI function wrapper: %s", tool.Raw)
	}
	if gotType := tool.Get("input_schema.type").String(); gotType != "object" {
		t.Fatalf("input_schema.type = %q, want object", gotType)
	}
}

func TestNormalizeClaudeToolsFromOpenAIResponsesSkipsBuiltInTools(t *testing.T) {
	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"","description":"","input_schema":{}}]}`)
	original := []byte(`{"model":"glm-5.2","input":"hi","tools":[{"type":"web_search"}]}`)

	got := normalizeClaudeToolsFromOpenAIResponses(body, original)
	if gjson.GetBytes(got, "tools").Exists() {
		t.Fatalf("built-in-only tools should be removed, got %s", gjson.GetBytes(got, "tools").Raw)
	}
}
