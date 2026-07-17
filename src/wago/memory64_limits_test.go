package wago

import (
	"strings"
	"testing"
)

func TestMemoryStatePreservesExactMemory64Maximum(t *testing.T) {
	base, err := NewMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()

	// Reuse the bounded mapping while installing the exact external declaration
	// that an instance export would publish.
	base.state.Store(&memoryState{})
	const declared = uint64(1)<<32 + 1
	if err := base.share(nil, memoryDef{Min: 1, Max: declared, HasMax: true, Addr64: true}); err != nil {
		t.Fatal(err)
	}
	state := base.state.Load()
	if got := state.declaredMaximum(); got != declared {
		t.Fatalf("declared maximum = %d, want %d", got, declared)
	}
	if err := base.validateLimits(1, declared, true, true); err != nil {
		t.Fatalf("compatible import rejected: %v", err)
	}
	if err := base.validateLimits(1, 10, true, true); err == nil || !strings.Contains(err.Error(), "exceeds required 10") {
		t.Fatalf("narrow incompatible import error = %v", err)
	}
}
