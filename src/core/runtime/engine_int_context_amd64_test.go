//go:build amd64 && !tinygo && (linux || darwin || windows)

package runtime

import "testing"

func TestPrepareBoundedIntContextBindsStableReentryStackAMD64(t *testing.T) {
	eng, jm, _ := fixture(t)
	linMem := jm.LinMemBase()
	jm.putU64(offTrapStackReentry, 0)
	eng.PrepareBoundedIntContext(linMem)
	if got, want := jm.getU64(offTrapStackReentry), uint64(eng.stackTop-40); got != want {
		t.Fatalf("trap reentry stack = %#x, want %#x", got, want)
	}
}
