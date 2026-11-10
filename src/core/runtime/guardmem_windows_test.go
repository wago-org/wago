//go:build windows && (amd64 || arm64) && wago_guardpage

package runtime

import "testing"

func TestGuardedLinearMemoryUsesWindowsAllocationAlignment(t *testing.T) {
	j, err := NewJobMemoryGuarded(0, wasmPageBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	if got := j.reserveBase + uintptr(j.linOff); got%wasmPageBytes != 0 {
		t.Fatalf("linear-memory base %#x is not %d-byte aligned", got, wasmPageBytes)
	}
}
