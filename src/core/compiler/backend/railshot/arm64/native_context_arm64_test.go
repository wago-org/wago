//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

// Call2 supplies only X0/X1. A non-leaf internal entry also needs X26: its
// stack-fence load reads basedata even when the guest has no linear memory.
// Deliberately poison that unspecified host register in an isolated process so
// this contract does not depend on whichever Go closure happened to run first.
func TestNonLeafInternalEntryNeedsRuntimeContextArm64(t *testing.T) {
	const child = "WAGO_TEST_INVALID_ARM64_INTERNAL_CONTEXT"
	if os.Getenv(child) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestNonLeafInternalEntryNeedsRuntimeContextArm64$")
		cmd.Env = append(os.Environ(), child+"=1")
		out, err := cmd.CombinedOutput()
		if err == nil || !bytes.Contains(out, []byte("deliberately invalid X26")) || (!bytes.Contains(out, []byte("fatal error:")) && !bytes.Contains(out, []byte("SIGSEGV"))) {
			t.Fatalf("invalid-context control = %v\n%s", err, out)
		}
		return
	}
	cm, err := CompileModuleWith(callFreeLoopExitModule(t), CompileOptions{Optimizations: map[string]bool{"inline": false}})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	var prefix enc.Asm
	prefix.MovImm64(enc.X26, 1) // deliberately invalid basedata pointer
	branch := prefix.Branch()
	if !prefix.PatchBranch26(branch, prefix.Len()+cm.InternalEntry[0]) {
		t.Fatal("internal entry is outside the control's branch range")
	}
	// Prepend the poison and branch without changing any relative references
	// within the compiled image. The first fence then dereferences invalid X26.
	image := append(prefix.B, cm.Code...)
	code, err := arm64spike.MapExec(image)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stderr, "entering non-leaf code with deliberately invalid X26")
	arm64spike.Call2(uintptr(unsafe.Pointer(&code[0])), 7, 0)
	t.Fatal("non-leaf internal entry unexpectedly ignored invalid runtime context")
}

func TestNativeFixtureRuntimeContextPreservesFenceArm64(t *testing.T) {
	// The proper runtime executor retains both the successful non-leaf path and
	// a real stack-fence trap. Changing the executor must not disable checks.
	for _, n := range []uint64{0, 1, 17, 255} {
		got, err := runArm64WrapperWithOptions(t, callFreeLoopExitModule(t), CompileOptions{Optimizations: map[string]bool{"inline": false}}, n)
		if err != nil || got != n {
			t.Fatalf("non-leaf runtime call(%d) = %d, %v", n, got, err)
		}
	}
	m := modFuncs(t, funcDef{nil, []wasm.ValType{wasm.I32}, []byte{0, 0x10, 0, 0x41, 1, 0x6a, 0x0b}})
	_, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"inline": false}})
	trap, ok := err.(*coreruntime.TrapError)
	if !ok || trap.Code != coreruntime.TrapStackFenceBreached {
		t.Fatalf("recursive runtime call = %v; want stack-fence trap", err)
	}
}
