package executor

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestCircuitBreakerFailureDeduperMarksOneFailurePerAuthAndModel(t *testing.T) {
	metadata := map[string]any{
		CircuitBreakerFailureDeduperMetadataKey: NewCircuitBreakerFailureDeduper(),
	}
	deduper := CircuitBreakerFailureDeduperFromMetadata(metadata)
	if deduper == nil {
		t.Fatal("request metadata should expose a deduper")
	}

	if !deduper.Mark("auth-1", "gpt-5.6-terra") {
		t.Fatal("first failure should be recorded")
	}
	if deduper.Mark("auth-1", "gpt-5.6-terra") {
		t.Fatal("duplicate failure should be skipped")
	}
	if !deduper.Mark("auth-2", "gpt-5.6-terra") {
		t.Fatal("different auth failure should be recorded")
	}
	if !deduper.Mark("auth-1", "gpt-5.6-mini") {
		t.Fatal("different model failure should be recorded")
	}
}

func TestCircuitBreakerFailureDeduperAllowsCallsWithoutRequestState(t *testing.T) {
	if deduper := CircuitBreakerFailureDeduperFromMetadata(nil); deduper != nil {
		t.Fatalf("missing request state returned %#v, want nil", deduper)
	}
}

func TestCircuitBreakerFailureDeduperIsConcurrentSafe(t *testing.T) {
	deduper := NewCircuitBreakerFailureDeduper()
	var recorded atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if deduper.Mark("auth-1", "gpt-5.6-terra") {
				recorded.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := recorded.Load(); got != 1 {
		t.Fatalf("recorded failures = %d, want 1", got)
	}
}
