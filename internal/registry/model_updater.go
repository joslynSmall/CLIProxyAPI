package registry

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	modelsFetchTimeout    = 30 * time.Second
	modelsRefreshInterval = 3 * time.Hour
)

var modelsURLs = []string{
	"https://raw.githubusercontent.com/joslynSmall/models/refs/heads/main/models.json",
}

//go:embed models/models.json
var embeddedModelsJSON []byte

type modelStore struct {
	mu   sync.RWMutex
	data *staticModelsJSON
}

type decodedModelsCatalog struct {
	catalog         *staticModelsJSON
	present         map[string]bool
	sectionErrors   map[string]error
	unknownSections []string
}

type modelCatalogMergeReport struct {
	appliedSections  []string
	preservedMissing []string
	invalidSections  map[string]error
	unknownSections  []string
}

type modelCatalogSection struct {
	name     string
	provider string
	get      func(*staticModelsJSON) []*ModelInfo
	set      func(*staticModelsJSON, []*ModelInfo)
}

var modelCatalogSections = []modelCatalogSection{
	{name: "claude", provider: "claude", get: func(c *staticModelsJSON) []*ModelInfo { return c.Claude }, set: func(c *staticModelsJSON, m []*ModelInfo) { c.Claude = m }},
	{name: "gemini", provider: "gemini", get: func(c *staticModelsJSON) []*ModelInfo { return c.Gemini }, set: func(c *staticModelsJSON, m []*ModelInfo) { c.Gemini = m }},
	{name: "vertex", provider: "vertex", get: func(c *staticModelsJSON) []*ModelInfo { return c.Vertex }, set: func(c *staticModelsJSON, m []*ModelInfo) { c.Vertex = m }},
	{name: "gemini-cli", provider: "gemini-cli", get: func(c *staticModelsJSON) []*ModelInfo { return c.GeminiCLI }, set: func(c *staticModelsJSON, m []*ModelInfo) { c.GeminiCLI = m }},
	{name: "aistudio", provider: "aistudio", get: func(c *staticModelsJSON) []*ModelInfo { return c.AIStudio }, set: func(c *staticModelsJSON, m []*ModelInfo) { c.AIStudio = m }},
	{name: "codex-free", provider: "codex", get: func(c *staticModelsJSON) []*ModelInfo { return c.CodexFree }, set: func(c *staticModelsJSON, m []*ModelInfo) { c.CodexFree = m }},
	{name: "codex-team", provider: "codex", get: func(c *staticModelsJSON) []*ModelInfo { return c.CodexTeam }, set: func(c *staticModelsJSON, m []*ModelInfo) { c.CodexTeam = m }},
	{name: "codex-plus", provider: "codex", get: func(c *staticModelsJSON) []*ModelInfo { return c.CodexPlus }, set: func(c *staticModelsJSON, m []*ModelInfo) { c.CodexPlus = m }},
	{name: "codex-pro", provider: "codex", get: func(c *staticModelsJSON) []*ModelInfo { return c.CodexPro }, set: func(c *staticModelsJSON, m []*ModelInfo) { c.CodexPro = m }},
	{name: "qwen", provider: "qwen", get: func(c *staticModelsJSON) []*ModelInfo { return c.Qwen }, set: func(c *staticModelsJSON, m []*ModelInfo) { c.Qwen = m }},
	{name: "iflow", provider: "iflow", get: func(c *staticModelsJSON) []*ModelInfo { return c.IFlow }, set: func(c *staticModelsJSON, m []*ModelInfo) { c.IFlow = m }},
	{name: "kimi", provider: "kimi", get: func(c *staticModelsJSON) []*ModelInfo { return c.Kimi }, set: func(c *staticModelsJSON, m []*ModelInfo) { c.Kimi = m }},
	{name: "antigravity", provider: "antigravity", get: func(c *staticModelsJSON) []*ModelInfo { return c.Antigravity }, set: func(c *staticModelsJSON, m []*ModelInfo) { c.Antigravity = m }},
}

func decodeModelsCatalog(data []byte) (*decodedModelsCatalog, error) {
	var rawSections map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawSections); err != nil {
		return nil, fmt.Errorf("decode top-level models catalog: %w", err)
	}
	if rawSections == nil {
		return nil, fmt.Errorf("decode top-level models catalog: expected object")
	}

	result := &decodedModelsCatalog{
		catalog:       &staticModelsJSON{},
		present:       make(map[string]bool),
		sectionErrors: make(map[string]error),
	}
	knownSections := make(map[string]struct{}, len(modelCatalogSections))
	for _, section := range modelCatalogSections {
		knownSections[section.name] = struct{}{}
		raw, ok := rawSections[section.name]
		if !ok {
			continue
		}
		result.present[section.name] = true
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || trimmed[0] != '[' {
			result.sectionErrors[section.name] = fmt.Errorf("decode %s section: expected array", section.name)
			continue
		}
		var models []*ModelInfo
		if err := json.Unmarshal(raw, &models); err != nil {
			result.sectionErrors[section.name] = fmt.Errorf("decode %s section: %w", section.name, err)
			continue
		}
		section.set(result.catalog, models)
	}

	for name := range rawSections {
		if _, ok := knownSections[name]; !ok {
			result.unknownSections = append(result.unknownSections, name)
		}
	}
	sort.Strings(result.unknownSections)
	return result, nil
}

func mergeDecodedModelsCatalog(current *staticModelsJSON, decoded *decodedModelsCatalog) (*staticModelsJSON, *modelCatalogMergeReport, error) {
	if current == nil {
		return nil, nil, fmt.Errorf("current models catalog is nil")
	}
	if decoded == nil || decoded.catalog == nil {
		return nil, nil, fmt.Errorf("decoded models catalog is nil")
	}

	merged := &staticModelsJSON{}
	report := &modelCatalogMergeReport{
		invalidSections: make(map[string]error),
		unknownSections: append([]string(nil), decoded.unknownSections...),
	}
	for _, section := range modelCatalogSections {
		currentModels := cloneModelInfos(section.get(current))
		if !decoded.present[section.name] {
			section.set(merged, currentModels)
			report.preservedMissing = append(report.preservedMissing, section.name)
			continue
		}
		if err := decoded.sectionErrors[section.name]; err != nil {
			section.set(merged, currentModels)
			report.invalidSections[section.name] = err
			continue
		}
		remoteModels := section.get(decoded.catalog)
		if err := validateModelSection(section.name, remoteModels); err != nil {
			section.set(merged, currentModels)
			report.invalidSections[section.name] = err
			continue
		}
		section.set(merged, cloneModelInfos(remoteModels))
		report.appliedSections = append(report.appliedSections, section.name)
	}
	if len(report.appliedSections) == 0 {
		return nil, report, fmt.Errorf("catalog contains no applicable supported sections")
	}
	if err := validateModelsCatalog(merged); err != nil {
		return nil, report, fmt.Errorf("validate merged models catalog: %w", err)
	}
	return merged, report, nil
}

func validateCompleteDecodedCatalog(decoded *decodedModelsCatalog) error {
	if decoded == nil || decoded.catalog == nil {
		return fmt.Errorf("catalog is nil")
	}
	for _, section := range modelCatalogSections {
		if !decoded.present[section.name] {
			return fmt.Errorf("%s section is missing", section.name)
		}
		if err := decoded.sectionErrors[section.name]; err != nil {
			return err
		}
	}
	return validateModelsCatalog(decoded.catalog)
}

var modelsCatalogStore = &modelStore{}

var updaterOnce sync.Once

// ModelRefreshCallback is invoked when startup or periodic model refresh detects changes.
// changedProviders contains the provider names whose model definitions changed.
type ModelRefreshCallback func(changedProviders []string)

var (
	refreshCallbackMu     sync.Mutex
	refreshCallback       ModelRefreshCallback
	pendingRefreshChanges []string
)

// SetModelRefreshCallback registers a callback that is invoked when startup or
// periodic model refresh detects changes. Only one callback is supported;
// subsequent calls replace the previous callback.
func SetModelRefreshCallback(cb ModelRefreshCallback) {
	refreshCallbackMu.Lock()
	refreshCallback = cb
	var pending []string
	if cb != nil && len(pendingRefreshChanges) > 0 {
		pending = append([]string(nil), pendingRefreshChanges...)
		pendingRefreshChanges = nil
	}
	refreshCallbackMu.Unlock()

	if cb != nil && len(pending) > 0 {
		cb(pending)
	}
}

func init() {
	// Load embedded data as fallback on startup.
	if err := loadModelsFromBytes(embeddedModelsJSON, "embed"); err != nil {
		panic(fmt.Sprintf("registry: failed to parse embedded models.json: %v", err))
	}
}

// StartModelsUpdater starts a background updater that fetches models
// immediately on startup and then refreshes the model catalog every 3 hours.
// Safe to call multiple times; only one updater will run.
func StartModelsUpdater(ctx context.Context) {
	updaterOnce.Do(func() {
		go runModelsUpdater(ctx)
	})
}

func runModelsUpdater(ctx context.Context) {
	tryStartupRefresh(ctx)
	periodicRefresh(ctx)
}

func periodicRefresh(ctx context.Context) {
	ticker := time.NewTicker(modelsRefreshInterval)
	defer ticker.Stop()
	log.Infof("periodic model refresh started (interval=%s)", modelsRefreshInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tryPeriodicRefresh(ctx)
		}
	}
}

// tryPeriodicRefresh fetches models from remote, compares with the current
// catalog, and notifies the registered callback if any provider changed.
func tryPeriodicRefresh(ctx context.Context) {
	tryRefreshModels(ctx, "periodic model refresh")
}

// tryStartupRefresh fetches models from remote in the background during
// process startup. It uses the same change detection as periodic refresh so
// existing auth registrations can be updated after the callback is registered.
func tryStartupRefresh(ctx context.Context) {
	tryRefreshModels(ctx, "startup model refresh")
}

func tryRefreshModels(ctx context.Context, label string) {
	oldData := getModels()

	parsed, url, report := fetchModelsFromRemote(ctx, oldData)
	if parsed == nil {
		log.Warnf("%s: fetch failed from all URLs, keeping current data", label)
		return
	}

	// Detect changes before updating store.
	changed := detectChangedProviders(oldData, parsed)

	// Update store with new data regardless.
	modelsCatalogStore.mu.Lock()
	modelsCatalogStore.data = parsed
	modelsCatalogStore.mu.Unlock()

	if len(report.preservedMissing) > 0 {
		log.Infof("%s preserved missing model sections from current catalog: %v", label, report.preservedMissing)
	}
	if len(report.unknownSections) > 0 {
		log.Debugf("%s ignored unknown model sections: %v", label, report.unknownSections)
	}
	if len(changed) == 0 {
		log.Infof("%s completed from %s, no changes detected", label, url)
		return
	}

	log.Infof("%s completed from %s, changes detected for providers: %v", label, url, changed)
	notifyModelRefresh(changed)
}

// fetchModelsFromRemote tries all remote URLs and merges each response section-by-section
// with current. It returns nil when no URL produces an applicable catalog.
func fetchModelsFromRemote(ctx context.Context, current *staticModelsJSON) (*staticModelsJSON, string, *modelCatalogMergeReport) {
	client := &http.Client{Timeout: modelsFetchTimeout}
	for _, url := range modelsURLs {
		reqCtx, cancel := context.WithTimeout(ctx, modelsFetchTimeout)
		req, err := http.NewRequestWithContext(reqCtx, "GET", url, nil)
		if err != nil {
			cancel()
			log.Debugf("models fetch request creation failed for %s: %v", url, err)
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			cancel()
			log.Debugf("models fetch failed from %s: %v", url, err)
			continue
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			cancel()
			log.Debugf("models fetch returned %d from %s", resp.StatusCode, url)
			continue
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		if err != nil {
			log.Debugf("models fetch read error from %s: %v", url, err)
			continue
		}

		decoded, err := decodeModelsCatalog(data)
		if err != nil {
			log.Warnf("models parse failed from %s: %v", url, err)
			continue
		}
		merged, report, err := mergeDecodedModelsCatalog(current, decoded)
		if err != nil {
			log.Warnf("models merge failed from %s: %v", url, err)
			continue
		}
		invalidNames := make([]string, 0, len(report.invalidSections))
		for name := range report.invalidSections {
			invalidNames = append(invalidNames, name)
		}
		sort.Strings(invalidNames)
		for _, name := range invalidNames {
			log.Warnf("models section %s from %s is invalid, keeping current section: %v", name, url, report.invalidSections[name])
		}

		return merged, url, report
	}
	return nil, "", nil
}

// detectChangedProviders compares two model catalogs and returns provider names
// whose model definitions differ. Codex tiers (free/team/plus/pro) are grouped
// under a single "codex" provider.
func detectChangedProviders(oldData, newData *staticModelsJSON) []string {
	if oldData == nil || newData == nil {
		return nil
	}

	seen := make(map[string]bool, len(modelCatalogSections))
	var changed []string
	for _, section := range modelCatalogSections {
		if seen[section.provider] {
			continue
		}
		if modelSectionChanged(section.get(oldData), section.get(newData)) {
			changed = append(changed, section.provider)
			seen[section.provider] = true
		}
	}
	return changed
}

// modelSectionChanged reports whether two model slices differ.
func modelSectionChanged(a, b []*ModelInfo) bool {
	if len(a) != len(b) {
		return true
	}
	if len(a) == 0 {
		return false
	}
	aj, err1 := json.Marshal(a)
	bj, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return true
	}
	return string(aj) != string(bj)
}

func notifyModelRefresh(changedProviders []string) {
	if len(changedProviders) == 0 {
		return
	}

	refreshCallbackMu.Lock()
	cb := refreshCallback
	if cb == nil {
		pendingRefreshChanges = mergeProviderNames(pendingRefreshChanges, changedProviders)
		refreshCallbackMu.Unlock()
		return
	}
	refreshCallbackMu.Unlock()
	cb(changedProviders)
}

func mergeProviderNames(existing, incoming []string) []string {
	if len(incoming) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	merged := make([]string, 0, len(existing)+len(incoming))
	for _, provider := range existing {
		name := strings.ToLower(strings.TrimSpace(provider))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		merged = append(merged, name)
	}
	for _, provider := range incoming {
		name := strings.ToLower(strings.TrimSpace(provider))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		merged = append(merged, name)
	}
	return merged
}

func loadModelsFromBytes(data []byte, source string) error {
	decoded, err := decodeModelsCatalog(data)
	if err != nil {
		return fmt.Errorf("%s: decode models catalog: %w", source, err)
	}
	if err = validateCompleteDecodedCatalog(decoded); err != nil {
		return fmt.Errorf("%s: validate models catalog: %w", source, err)
	}

	modelsCatalogStore.mu.Lock()
	modelsCatalogStore.data = decoded.catalog
	modelsCatalogStore.mu.Unlock()
	return nil
}

func getModels() *staticModelsJSON {
	modelsCatalogStore.mu.RLock()
	defer modelsCatalogStore.mu.RUnlock()
	return modelsCatalogStore.data
}

func validateModelsCatalog(data *staticModelsJSON) error {
	if data == nil {
		return fmt.Errorf("catalog is nil")
	}
	for _, section := range modelCatalogSections {
		if err := validateModelSection(section.name, section.get(data)); err != nil {
			return err
		}
	}
	return nil
}

func validateModelSection(section string, models []*ModelInfo) error {
	seen := make(map[string]struct{}, len(models))
	for i, model := range models {
		if model == nil {
			return fmt.Errorf("%s[%d] is null", section, i)
		}
		modelID := strings.TrimSpace(model.ID)
		if modelID == "" {
			return fmt.Errorf("%s[%d] has empty id", section, i)
		}
		if _, exists := seen[modelID]; exists {
			return fmt.Errorf("%s contains duplicate model id %q", section, modelID)
		}
		seen[modelID] = struct{}{}
	}
	return nil
}
