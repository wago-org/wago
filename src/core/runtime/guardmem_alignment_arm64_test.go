//go:build wago_guardpage && (linux || darwin) && arm64

package runtime

import "testing"

func TestGuardedLinearMemoryIsWasmPageAligned(t *testing.T) {
	j, err := NewJobMemoryGuarded(wasmPageBytes, 2*wasmPageBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	linMem := j.reserveBase + uintptr(j.linOff)
	if linMem&uintptr(wasmPageBytes-1) != 0 {
		t.Fatalf("guarded linear memory base %#x is not %d-byte aligned", linMem, wasmPageBytes)
	}
	if j.linOff < basedataSize {
		t.Fatalf("guarded linear-memory offset %d overlaps %d-byte basedata", j.linOff, basedataSize)
	}
}
