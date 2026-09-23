//go:build arm64

package arm64

import "testing"

func TestCallSafePinPoolExcludesCallerSavedRegistersAfterReservations(t *testing.T) {
	pool := []Reg{X19, X20, X21, X23, X24, X9, X10, X11, X15, X27}
	got := callSafePinPool(pool)
	want := [...]Reg{X19, X20, X21, X23, X24}
	if len(got) != len(want) {
		t.Fatalf("safe pool = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("safe pool = %v, want %v", got, want)
		}
	}
}
