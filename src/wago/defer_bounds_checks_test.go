//go:build linux && amd64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// twoLoadModule: func(p i32){ i32.load(p+4); drop; i32.load(p+0); drop } over a
// 1-page memory. The second load is within the extent the first check proved, so
// P6.1 elides it in explicit mode.
func twoLoadModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})), // memory: min 1 page
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x28, 0x02, 0x04, 0x1a, // local.get 0; i32.load off=4; drop
			0x20, 0x00, 0x28, 0x02, 0x00, 0x1a, // local.get 0; i32.load off=0; drop
			0x0b,
		}))),
	)
}

func TestWithDeferBoundsChecks(t *testing.T) {
	// Force explicit mode so the facts machinery applies regardless of build tag.
	base := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	if !base.DeferBoundsChecks() {
		t.Fatal("defer-bounds-checks should default on")
	}
	off := base.WithDeferBoundsChecks(false)
	if off.DeferBoundsChecks() {
		t.Fatal("WithDeferBoundsChecks(false) did not disable")
	}
	if !base.DeferBoundsChecks() {
		t.Fatal("WithDeferBoundsChecks mutated the base config")
	}

	// The option reaches codegen through the public API: eliding the redundant
	// second check shrinks the emitted code.
	on, err := CompileWithConfig(base, twoLoadModule())
	if err != nil {
		t.Fatal(err)
	}
	dis, err := CompileWithConfig(off, twoLoadModule())
	if err != nil {
		t.Fatal(err)
	}
	if len(on.Code) >= len(dis.Code) {
		t.Errorf("elision-on code (%d B) not smaller than facts-off (%d B)", len(on.Code), len(dis.Code))
	}
}
