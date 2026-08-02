package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestMergeDecodedModelsCatalogAppliesThreeStateProtocol(t *testing.T) {
	current := &staticModelsJSON{
		CodexPlus: []*ModelInfo{{ID: "gpt-old"}},
		Qwen:      []*ModelInfo{{ID: "qwen-old"}},
		IFlow:     []*ModelInfo{{ID: "iflow-old"}},
		Kimi:      []*ModelInfo{{ID: "kimi-old"}},
	}
	decoded, err := decodeModelsCatalog([]byte(`{
		"codex-plus": [{"id": "gpt-new"}],
		"qwen": [],
		"iflow": [],
		"xai": [{"id": "grok-unknown"}]
	}`))
	if err != nil {
		t.Fatalf("decodeModelsCatalog() error = %v", err)
	}

	merged, report, err := mergeDecodedModelsCatalog(current, decoded)
	if err != nil {
		t.Fatalf("mergeDecodedModelsCatalog() error = %v", err)
	}
	if len(merged.CodexPlus) != 1 || merged.CodexPlus[0].ID != "gpt-new" {
		t.Fatalf("codex-plus = %#v, want replacement", merged.CodexPlus)
	}
	if len(merged.Qwen) != 0 {
		t.Fatalf("qwen = %#v, want explicit clear", merged.Qwen)
	}
	if len(merged.IFlow) != 0 {
		t.Fatalf("iflow = %#v, want explicit clear", merged.IFlow)
	}
	if len(merged.Kimi) != 1 || merged.Kimi[0].ID != "kimi-old" {
		t.Fatalf("kimi = %#v, want missing section preserved", merged.Kimi)
	}
	if !containsString(report.appliedSections, "codex-plus") || !containsString(report.appliedSections, "qwen") || !containsString(report.appliedSections, "iflow") {
		t.Fatalf("applied sections = %v", report.appliedSections)
	}
	if !containsString(report.preservedMissing, "kimi") {
		t.Fatalf("preserved missing sections = %v", report.preservedMissing)
	}
	if !reflect.DeepEqual(report.unknownSections, []string{"xai"}) {
		t.Fatalf("unknown sections = %v, want [xai]", report.unknownSections)
	}
	if got := detectChangedProviders(current, merged); !reflect.DeepEqual(got, []string{"codex", "qwen", "iflow"}) {
		t.Fatalf("changed providers = %v, want [codex qwen iflow]", got)
	}
}

func TestMergeDecodedModelsCatalogPreservesOnlyInvalidSection(t *testing.T) {
	current := &staticModelsJSON{
		CodexPlus: []*ModelInfo{{ID: "gpt-old"}},
		Qwen:      []*ModelInfo{{ID: "qwen-old"}},
	}
	decoded, err := decodeModelsCatalog([]byte(`{
		"codex-plus": [{"id": "gpt-new"}],
		"qwen": [{"id": "duplicate"}, {"id": "duplicate"}]
	}`))
	if err != nil {
		t.Fatalf("decodeModelsCatalog() error = %v", err)
	}

	merged, report, err := mergeDecodedModelsCatalog(current, decoded)
	if err != nil {
		t.Fatalf("mergeDecodedModelsCatalog() error = %v", err)
	}
	if len(merged.CodexPlus) != 1 || merged.CodexPlus[0].ID != "gpt-new" {
		t.Fatalf("codex-plus = %#v, want valid replacement", merged.CodexPlus)
	}
	if len(merged.Qwen) != 1 || merged.Qwen[0].ID != "qwen-old" {
		t.Fatalf("qwen = %#v, want invalid section preserved", merged.Qwen)
	}
	if report.invalidSections["qwen"] == nil {
		t.Fatalf("invalid sections = %v, want qwen error", report.invalidSections)
	}
}

func TestMergeDecodedModelsCatalogPreservesNullSection(t *testing.T) {
	current := &staticModelsJSON{Qwen: []*ModelInfo{{ID: "qwen-old"}}}
	decoded, err := decodeModelsCatalog([]byte(`{"qwen":null,"codex-plus":[{"id":"gpt-new"}]}`))
	if err != nil {
		t.Fatalf("decodeModelsCatalog() error = %v", err)
	}
	if decoded.sectionErrors["qwen"] == nil {
		t.Fatal("null qwen section should be rejected as non-array")
	}
	merged, report, err := mergeDecodedModelsCatalog(current, decoded)
	if err != nil {
		t.Fatalf("mergeDecodedModelsCatalog() error = %v", err)
	}
	if len(merged.Qwen) != 1 || merged.Qwen[0].ID != "qwen-old" {
		t.Fatalf("qwen = %#v, want null section preserved", merged.Qwen)
	}
	if report.invalidSections["qwen"] == nil {
		t.Fatalf("invalid sections = %v, want qwen error", report.invalidSections)
	}
}

func TestMergeDecodedModelsCatalogRejectsUnknownOnlyPatch(t *testing.T) {
	decoded, err := decodeModelsCatalog([]byte(`{"xai": [{"id": "grok-test"}]}`))
	if err != nil {
		t.Fatalf("decodeModelsCatalog() error = %v", err)
	}
	if _, _, err = mergeDecodedModelsCatalog(&staticModelsJSON{}, decoded); err == nil {
		t.Fatal("mergeDecodedModelsCatalog() error = nil, want no applicable sections error")
	}
}

func TestValidateCompleteDecodedCatalogRequiresExplicitSectionsAndAllowsEmpty(t *testing.T) {
	raw := make(map[string]any, len(modelCatalogSections))
	for _, section := range modelCatalogSections {
		raw[section.name] = []any{}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal complete catalog: %v", err)
	}
	decoded, err := decodeModelsCatalog(data)
	if err != nil {
		t.Fatalf("decodeModelsCatalog() error = %v", err)
	}
	if err = validateCompleteDecodedCatalog(decoded); err != nil {
		t.Fatalf("validateCompleteDecodedCatalog() error = %v", err)
	}

	delete(raw, "iflow")
	data, err = json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal incomplete catalog: %v", err)
	}
	decoded, err = decodeModelsCatalog(data)
	if err != nil {
		t.Fatalf("decode incomplete catalog: %v", err)
	}
	if err = validateCompleteDecodedCatalog(decoded); err == nil || !strings.Contains(err.Error(), "iflow section is missing") {
		t.Fatalf("validate incomplete catalog error = %v, want missing iflow", err)
	}
}

func TestFetchModelsFromRemoteMergesValidSections(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"codex-plus":[{"id":"gpt-new"}],"qwen":[],"iflow":[],"xai":[]}`))
	}))
	defer server.Close()

	originalURLs := modelsURLs
	modelsURLs = []string{server.URL}
	t.Cleanup(func() { modelsURLs = originalURLs })
	current := &staticModelsJSON{
		CodexPlus: []*ModelInfo{{ID: "gpt-old"}},
		Qwen:      []*ModelInfo{{ID: "qwen-old"}},
		IFlow:     []*ModelInfo{{ID: "iflow-old"}},
		Kimi:      []*ModelInfo{{ID: "kimi-old"}},
	}

	merged, source, report := fetchModelsFromRemote(context.Background(), current)
	if merged == nil || report == nil {
		t.Fatal("fetchModelsFromRemote() returned no catalog")
	}
	if source != server.URL {
		t.Fatalf("source = %q, want %q", source, server.URL)
	}
	if len(merged.CodexPlus) != 1 || merged.CodexPlus[0].ID != "gpt-new" || len(merged.Qwen) != 0 {
		t.Fatalf("merged catalog = %#v", merged)
	}
	if len(merged.IFlow) != 0 {
		t.Fatalf("iflow = %#v, want explicit clear", merged.IFlow)
	}
	if len(merged.Kimi) != 1 || merged.Kimi[0].ID != "kimi-old" {
		t.Fatalf("kimi = %#v, want preserved", merged.Kimi)
	}
}

func TestEmbeddedCatalogSatisfiesSectionStateContract(t *testing.T) {
	decoded, err := decodeModelsCatalog(embeddedModelsJSON)
	if err != nil {
		t.Fatalf("decode embedded catalog: %v", err)
	}
	if err = validateCompleteDecodedCatalog(decoded); err != nil {
		t.Fatalf("validate embedded catalog: %v", err)
	}
	if got := GetQwenModels(); len(got) != 0 {
		t.Fatalf("qwen count = %d, want explicit retirement", len(got))
	}
	if got := GetIFlowModels(); len(got) != 0 {
		t.Fatalf("iflow count = %d, want explicit retirement", len(got))
	}
	if LookupStaticModelInfo("gpt-5.5") == nil {
		t.Fatal("embedded catalog should contain gpt-5.5 fixture")
	}
}

func TestModelsURLsUseForkCatalog(t *testing.T) {
	if len(modelsURLs) != 1 || !strings.Contains(modelsURLs[0], "githubusercontent.com/joslynSmall/models/") {
		t.Fatalf("modelsURLs = %v, want joslynSmall fork only", modelsURLs)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
