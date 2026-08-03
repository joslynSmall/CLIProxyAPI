package logging

import (
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

func TestLogFormatter_ReasoningCompatibilityEventRendersOnlyAllowedFields(t *testing.T) {
	entry := &log.Entry{
		Data: log.Fields{
			"request_id":       "req-reasoning-compatibility",
			"endpoint":         "/v1/responses",
			"protocol":         "openai-responses",
			"provider":         "codex",
			"model":            "gpt-5.6-terra",
			"action":           "accepted",
			"reason":           "legacy_alias_normalized",
			"prompt":           "summarize the private meeting notes",
			"authorization":    "Bearer not-a-real-token",
			"access_token":     "not-a-real-access-token",
			"refresh_token":    "not-a-real-refresh-token",
			"reasoning_effort": "xhigh",
			"raw_payload":      `{"input":"full request body"}`,
			"account_id":       "unapproved-account-field",
		},
		Time:    time.Date(2026, time.August, 3, 9, 48, 0, 0, time.UTC),
		Level:   log.InfoLevel,
		Message: reasoningCompatibilityEventMessage,
	}

	formatted, err := (&LogFormatter{}).Format(entry)
	if err != nil {
		t.Fatalf("Format() error: %v", err)
	}
	output := string(formatted)

	for _, want := range []string{
		"request_id=req-reasoning-compatibility",
		"endpoint=/v1/responses",
		"protocol=openai-responses",
		"provider=codex",
		"model=gpt-5.6-terra",
		"action=accepted",
		"reason=legacy_alias_normalized",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("formatted event missing allowed field %q: %s", want, output)
		}
	}

	for _, forbidden := range []string{
		"prompt=",
		"summarize the private meeting notes",
		"authorization=",
		"Bearer not-a-real-token",
		"access_token=",
		"not-a-real-access-token",
		"refresh_token=",
		"not-a-real-refresh-token",
		"reasoning_effort=",
		"xhigh",
		"raw_payload=",
		"full request body",
		"account_id=",
		"unapproved-account-field",
	} {
		if strings.Contains(output, forbidden) {
			t.Errorf("formatted event leaked forbidden content %q: %s", forbidden, output)
		}
	}
}

func TestLogFormatter_OnlyExactCompatibilityMessageUsesCompatibilityFields(t *testing.T) {
	entry := &log.Entry{
		Data: log.Fields{
			"provider": "codex",
			"model":    "gpt-5.6-terra",
			"endpoint": "/v1/responses",
			"action":   "accepted",
			"reason":   "legacy_alias_normalized",
		},
		Time:    time.Date(2026, time.August, 3, 9, 48, 0, 0, time.UTC),
		Level:   log.InfoLevel,
		Message: reasoningCompatibilityEventMessage + "\n",
	}

	formatted, err := (&LogFormatter{}).Format(entry)
	if err != nil {
		t.Fatalf("Format() error: %v", err)
	}
	output := string(formatted)

	for _, want := range []string{"provider=codex", "model=gpt-5.6-terra"} {
		if !strings.Contains(output, want) {
			t.Errorf("formatted general event missing %q: %s", want, output)
		}
	}
	for _, forbidden := range []string{"endpoint=", "action=", "reason="} {
		if strings.Contains(output, forbidden) {
			t.Errorf("non-exact compatibility event rendered compatibility field %q: %s", forbidden, output)
		}
	}
}
