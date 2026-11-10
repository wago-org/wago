//go:build amd64

package amd64

import "testing"

func TestCompileMemoryPressureCheckpointRunsOnce(t *testing.T) {
	m := benchSmallScalarModule(t)
	calls := 0
	if _, err := CompileModuleWith(m, CompileOptions{MemoryPressureAt: 1, MemoryPressure: func() { calls++ }}); err != nil {
		t.Fatalf("CompileModuleWith: %v", err)
	}
	if calls != 1 {
		t.Fatalf("memory-pressure calls = %d, want 1", calls)
	}
}
