package executor

import (
	"fmt"
	"strings"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func carryResponsesReasoningToOpenAIChatToolCalls(from, to sdktranslator.Format, responsesPayload, chatPayload []byte) []byte {
	if from != sdktranslator.FormatOpenAIResponse || to != sdktranslator.FormatOpenAI || len(responsesPayload) == 0 || len(chatPayload) == 0 {
		return chatPayload
	}

	reasoningByToolGroup := responsesReasoningForFunctionCallGroups(responsesPayload)
	if len(reasoningByToolGroup) == 0 {
		return chatPayload
	}

	messages := gjson.GetBytes(chatPayload, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return chatPayload
	}

	out := chatPayload
	groupIdx := 0
	for msgIdx, msg := range messages.Array() {
		if groupIdx >= len(reasoningByToolGroup) {
			break
		}
		if strings.TrimSpace(msg.Get("role").String()) != "assistant" {
			continue
		}
		toolCalls := msg.Get("tool_calls")
		if !toolCalls.Exists() || !toolCalls.IsArray() || len(toolCalls.Array()) == 0 {
			continue
		}

		reasoning := strings.TrimSpace(reasoningByToolGroup[groupIdx])
		groupIdx++
		if reasoning == "" {
			continue
		}

		current := strings.TrimSpace(msg.Get("reasoning_content").String())
		if current != "" && current != "[reasoning unavailable]" {
			continue
		}
		if next, err := sjson.SetBytes(out, fmt.Sprintf("messages.%d.reasoning_content", msgIdx), reasoning); err == nil {
			out = next
		}
	}

	return out
}

func responsesReasoningForFunctionCallGroups(payload []byte) []string {
	input := gjson.GetBytes(payload, "input")
	if !input.Exists() || !input.IsArray() {
		return nil
	}

	latestReasoning := ""
	pendingToolGroup := false
	groups := make([]string, 0)
	input.ForEach(func(_, item gjson.Result) bool {
		itemType := strings.TrimSpace(item.Get("type").String())
		if itemType == "" && strings.TrimSpace(item.Get("role").String()) != "" {
			itemType = "message"
		}

		switch itemType {
		case "reasoning":
			if text := responsesReasoningSummaryText(item); text != "" {
				latestReasoning = text
			}
			pendingToolGroup = false
		case "function_call":
			if !pendingToolGroup {
				groups = append(groups, latestReasoning)
				pendingToolGroup = true
			}
		case "function_call_output", "message", "":
			pendingToolGroup = false
		default:
			pendingToolGroup = false
		}
		return true
	})

	return groups
}

func responsesReasoningSummaryText(item gjson.Result) string {
	summary := item.Get("summary")
	if !summary.Exists() || !summary.IsArray() {
		return ""
	}

	parts := make([]string, 0, len(summary.Array()))
	summary.ForEach(func(_, part gjson.Result) bool {
		text := strings.TrimSpace(part.Get("text").String())
		if text != "" {
			parts = append(parts, text)
		}
		return true
	})

	return strings.Join(parts, "\n\n")
}
