package registry

import (
	"strings"
	"testing"
)

func TestDecodeModelsCatalogDistinguishesSectionStates(t *testing.T) {
	decoded, err := decodeModelsCatalog([]byte(`{
		"codex-plus": [],
		"qwen": [{"id": "qwen-test"}],
		"xai": [{"id": "grok-test"}]
	}`))
	if err != nil {
		t.Fatalf("decodeModelsCatalog() error = %v", err)
	}

	if !decoded.present["codex-plus"] {
		t.Fatal("explicit empty codex-plus section should be present")
	}
	if decoded.catalog.CodexPlus == nil || len(decoded.catalog.CodexPlus) != 0 {
		t.Fatalf("codex-plus = %#v, want a decoded empty slice", decoded.catalog.CodexPlus)
	}
	if !decoded.present["qwen"] || len(decoded.catalog.Qwen) != 1 || decoded.catalog.Qwen[0].ID != "qwen-test" {
		t.Fatalf("qwen = %#v, want one decoded model", decoded.catalog.Qwen)
	}
	if decoded.present["iflow"] {
		t.Fatal("missing iflow section should not be present")
	}
	if decoded.catalog.IFlow != nil {
		t.Fatalf("missing iflow section = %#v, want nil", decoded.catalog.IFlow)
	}
	if len(decoded.sectionErrors) != 0 {
		t.Fatalf("section errors = %v, want none", decoded.sectionErrors)
	}
	if len(decoded.unknownSections) != 1 || decoded.unknownSections[0] != "xai" {
		t.Fatalf("unknown sections = %v, want [xai]", decoded.unknownSections)
	}
}

func TestDecodeModelsCatalogIsolatesSectionDecodeErrors(t *testing.T) {
	decoded, err := decodeModelsCatalog([]byte(`{
		"codex-plus": [{"id": "gpt-test"}],
		"qwen": {"id": "wrong-shape"},
		"iflow": []
	}`))
	if err != nil {
		t.Fatalf("decodeModelsCatalog() error = %v", err)
	}

	if len(decoded.catalog.CodexPlus) != 1 || decoded.catalog.CodexPlus[0].ID != "gpt-test" {
		t.Fatalf("codex-plus = %#v, want valid section to remain decoded", decoded.catalog.CodexPlus)
	}
	if !decoded.present["qwen"] {
		t.Fatal("invalid qwen section should still be marked present")
	}
	if decoded.sectionErrors["qwen"] == nil {
		t.Fatal("invalid qwen section should have a section-scoped error")
	}
	if decoded.catalog.Qwen != nil {
		t.Fatalf("invalid qwen section = %#v, want no partial decoded data", decoded.catalog.Qwen)
	}
	if !strings.Contains(decoded.sectionErrors["qwen"].Error(), "decode qwen section") {
		t.Fatalf("qwen error = %v, want section context", decoded.sectionErrors["qwen"])
	}
	if decoded.sectionErrors["codex-plus"] != nil || decoded.sectionErrors["iflow"] != nil {
		t.Fatalf("valid section errors = %v", decoded.sectionErrors)
	}
}

func TestDecodeModelsCatalogRejectsInvalidTopLevelJSON(t *testing.T) {
	tests := map[string]string{
		"malformed": `{"codex-plus":`,
		"array":     `[]`,
		"null":      `null`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeModelsCatalog([]byte(input)); err == nil {
				t.Fatal("decodeModelsCatalog() error = nil, want top-level error")
			}
		})
	}
}
