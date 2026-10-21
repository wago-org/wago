//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestNativeFixtureRuntimeContextPreservesFenceArm64(t *testing.T) {
	// The proper runtime executor retains both the successful non-leaf path and
	// a real stack-fence trap. Changing the executor must not disable checks.
	for _, n := range []uint64{0, 1, 17, 255} {
		got, err := runArm64WrapperWithOptions(t, callFreeLoopExitModule(t), CompileOptions{Optimizations: map[string]bool{"inline": false, "stack-fence": true}}, n)
		if err != nil || got != n {
			t.Fatalf("non-leaf runtime call(%d) = %d, %v", n, got, err)
		}
	}
	m := modFuncs(t, funcDef{nil, []wasm.ValType{wasm.I32}, []byte{0, 0x10, 0, 0x41, 1, 0x6a, 0x0b}})
	_, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"inline": false, "stack-fence": true}})
	trap, ok := err.(*coreruntime.TrapError)
	if !ok || trap.Code != coreruntime.TrapStackFenceBreached {
		t.Fatalf("recursive runtime call = %v; want stack-fence trap", err)
	}
}
