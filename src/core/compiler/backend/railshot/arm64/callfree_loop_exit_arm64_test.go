//go:build arm64

package arm64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

func callFreeLoopExitModule(t testing.TB) *wasm.Module {
	t.Helper()
	i32 := []wasm.ValType{wasm.I32}
	return modFuncs(t,
		funcDef{i32, i32, []byte{
			0x01, 0x01, 0x7f, // one i32 local
			0x20, 0x00, 0x21, 0x01, // x = n
			0x02, 0x40, // block
			0x03, 0x40, // loop
			0x20, 0x01, 0x45, 0x0d, 0x01, // br_if block (x == 0)
			0x20, 0x01, 0x41, 0x01, 0x6b, 0x21, 0x01, // x--
			0x0c, 0x00, // br loop
			0x0b, 0x0b,
			0x20, 0x00, 0x10, 0x01, // return identity(n), keeping f call-making
			0x0b,
		}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x0b}},
	)
}

func TestCallFreeLoopExitReconciliationIsColdArm64(t *testing.T) {
	m := callFreeLoopExitModule(t)
	compile := func(enabled bool) (*ModuleStats, int) {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{
			Stats: &stats,
			Optimizations: map[string]bool{
				"inline":                  false,
				"callfree-loop-cold-exit": enabled,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			_ = cm.CodeImage.Close()
		}
		return &stats, len(cm.Code)
	}
	on, onBytes := compile(true)
	off, offBytes := compile(false)
	if got := on.Funcs[0].Peephole["callfree-loop-exit-cold"]; got != 1 {
		t.Fatalf("cold loop exits = %d, want 1 (all: %v)", got, on.Funcs[0].Peephole)
	}
	if got := off.Funcs[0].Peephole["callfree-loop-exit-cold"]; got != 0 {
		t.Fatalf("disabled cold loop exits = %d, want 0", got)
	}
	// The cold edge adds one branch but must stay within that one-word cost.
	if onBytes > offBytes+4 {
		t.Fatalf("cold loop exit code = %d bytes, eager = %d", onBytes, offBytes)
	}
}

func TestCallFreeLoopExitExecArm64(t *testing.T) {
	m := callFreeLoopExitModule(t)
	cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"inline": false}})
	if err != nil {
		t.Fatal(err)
	}
	code, err := arm64spike.MapExec(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))
	for _, n := range []uintptr{0, 1, 2, 17, 255} {
		if got := arm64spike.Call2(entry, n, 0); uint32(got) != uint32(n) {
			t.Fatalf("f(%d) = %d, want %d", n, uint32(got), n)
		}
	}
}
