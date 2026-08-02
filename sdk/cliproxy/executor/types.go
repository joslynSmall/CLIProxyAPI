package executor

import (
	"net/http"
	"net/url"
	"strings"
	"sync"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

// RequestedModelMetadataKey stores the client-requested model name in Options.Metadata.
const RequestedModelMetadataKey = "requested_model"

const (
	// IngressRequestedModelMetadataKey stores the original model requested by the client.
	IngressRequestedModelMetadataKey = "ingress_requested_model"
	// IngressAPIKeyMetadataKey stores the ingress API key associated with the request.
	IngressAPIKeyMetadataKey = "ingress_api_key"
	// SessionAffinityMetadataKey stores the session-affinity identifier for request-level suppression.
	SessionAffinityMetadataKey = "session_affinity"
	// AvailabilityCacheHitMetadataKey marks that the request was short-circuited by the suppression cache.
	AvailabilityCacheHitMetadataKey = "availability_cache_hit"
	// SelectedUpstreamModelMetadataKey stores the upstream model chosen for the current execution attempt.
	SelectedUpstreamModelMetadataKey = "selected_upstream_model"
	// ExecutionMetadataContextKey stores the shared execution metadata map on the Gin context.
	ExecutionMetadataContextKey = "cliproxy_execution_metadata"
	// PinnedAuthMetadataKey locks execution to a specific auth ID.
	PinnedAuthMetadataKey = "pinned_auth_id"
	// SelectedAuthMetadataKey stores the auth ID selected by the scheduler.
	SelectedAuthMetadataKey = "selected_auth_id"
	// SelectedAuthCallbackMetadataKey carries an optional callback invoked with the selected auth ID.
	SelectedAuthCallbackMetadataKey = "selected_auth_callback"
	// ExecutionSessionMetadataKey identifies a long-lived downstream execution session.
	ExecutionSessionMetadataKey = "execution_session_id"
	// CircuitBreakerFailureDeduperMetadataKey stores request-scoped circuit-breaker failure state.
	CircuitBreakerFailureDeduperMetadataKey = "circuit_breaker_failure_deduper"
)

// CircuitBreakerFailureDeduper tracks circuit-breaker failures for one logical request.
type CircuitBreakerFailureDeduper struct {
	mu       sync.Mutex
	recorded map[circuitBreakerFailureScope]struct{}
}

type circuitBreakerFailureScope struct {
	authID string
	model  string
}

// NewCircuitBreakerFailureDeduper creates request-scoped state for circuit-breaker failure recording.
func NewCircuitBreakerFailureDeduper() *CircuitBreakerFailureDeduper {
	return &CircuitBreakerFailureDeduper{
		recorded: make(map[circuitBreakerFailureScope]struct{}),
	}
}

// CircuitBreakerFailureDeduperFromMetadata returns request-scoped failure state when present.
func CircuitBreakerFailureDeduperFromMetadata(metadata map[string]any) *CircuitBreakerFailureDeduper {
	if metadata == nil {
		return nil
	}
	deduper, _ := metadata[CircuitBreakerFailureDeduperMetadataKey].(*CircuitBreakerFailureDeduper)
	return deduper
}

// Mark returns true when a failure has not yet been recorded for this request.
func (d *CircuitBreakerFailureDeduper) Mark(authID, model string) bool {
	if d == nil {
		return true
	}
	scope := circuitBreakerFailureScope{
		authID: strings.TrimSpace(authID),
		model:  strings.TrimSpace(model),
	}
	if scope.authID == "" || scope.model == "" {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.recorded[scope]; exists {
		return false
	}
	d.recorded[scope] = struct{}{}
	return true
}

// Request encapsulates the translated payload that will be sent to a provider executor.
type Request struct {
	// Model is the upstream model identifier after translation.
	Model string
	// Payload is the provider specific JSON payload.
	Payload []byte
	// Format represents the provider payload schema.
	Format sdktranslator.Format
	// Metadata carries optional provider specific execution hints.
	Metadata map[string]any
}

// Options controls execution behavior for both streaming and non-streaming calls.
type Options struct {
	// Stream toggles streaming mode.
	Stream bool
	// Alt carries optional alternate format hint (e.g. SSE JSON key).
	Alt string
	// Headers are forwarded to the provider request builder.
	Headers http.Header
	// Query contains optional query string parameters.
	Query url.Values
	// OriginalRequest preserves the inbound request bytes prior to translation.
	OriginalRequest []byte
	// SourceFormat identifies the inbound schema.
	SourceFormat sdktranslator.Format
	// Metadata carries extra execution hints shared across selection and executors.
	Metadata map[string]any
}

// Response wraps either a full provider response or metadata for streaming flows.
type Response struct {
	// Payload is the provider response in the executor format.
	Payload []byte
	// Metadata exposes optional structured data for translators.
	Metadata map[string]any
	// Headers carries upstream HTTP response headers for passthrough to clients.
	Headers http.Header
}

// StreamChunk represents a single streaming payload unit emitted by provider executors.
type StreamChunk struct {
	// Payload is the raw provider chunk payload.
	Payload []byte
	// Err reports any terminal error encountered while producing chunks.
	Err error
}

// StreamResult wraps the streaming response, providing both the chunk channel
// and the upstream HTTP response headers captured before streaming begins.
type StreamResult struct {
	// Headers carries upstream HTTP response headers from the initial connection.
	Headers http.Header
	// Chunks is the channel of streaming payload units.
	Chunks <-chan StreamChunk
}

// StatusError represents an error that carries an HTTP-like status code.
// Provider executors should implement this when possible to enable
// better auth state updates on failures (e.g., 401/402/429).
type StatusError interface {
	error
	StatusCode() int
}
