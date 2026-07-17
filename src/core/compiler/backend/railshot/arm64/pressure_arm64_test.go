//go:build arm64

package arm64

import "testing"

func TestCompileMemoryPressureCheckpointRunsOnce(t *testing.T) {
	m := mod1(t, nil, nil, []byte{0x00, 0x01, 0x0b})
	calls := 0
	if _, err := CompileModuleWith(m, CompileOptions{MemoryPressureAt: 1, MemoryPressure: func() { calls++ }}); err != nil {
		t.Fatalf("CompileModuleWith: %v", err)
	}
	if calls != 1 {
		t.Fatalf("memory-pressure calls = %d, want 1", calls)
	}
}
