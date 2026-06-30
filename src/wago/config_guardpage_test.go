//go:build linux && amd64 && wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// loadModule exports f(i32)->i32 = i32.load(local0) over a 1-page memory.
func loadModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})), // memory: min 1 page
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}))),
	)
}

// TestConfigSignalsBasedEndToEnd drives guard-page mode through the full public
// API: CompileWithConfig -> Instantiate -> Invoke, with the OOB access trapping
// via the signal handler rather than an inline check.
func TestConfigSignalsBasedEndToEnd(t *testing.T) {
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	c, err := CompileWithConfig(cfg, loadModule())
	if err != nil {
		t.Fatal(err)
	}
	if c.boundsMode != BoundsChecksSignalsBased {
		t.Fatal("compiled module did not record signals-based mode")
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	res, err := in.Invoke("f", I32(8)) // in-bounds
	if err != nil {
		t.Fatalf("in-bounds load: %v", err)
	}
	if res[0].AsI32() != 0 {
		t.Fatalf("in-bounds load = %d, want 0", res[0].AsI32())
	}
	if _, err := in.Invoke("f", I32(1<<20)); err == nil { // OOB -> guard-page trap
		t.Fatal("out-of-bounds load did not trap")
	}
}

// TestSignalsBasedNotSerializable documents the footgun guard: an elided module
// must not be silently serialized (a loaded blob can't record that it needs a
// guard-page memory).
func TestSignalsBasedNotSerializable(t *testing.T) {
	c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased).Compile(loadModule())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.MarshalBinary(); err == nil {
		t.Fatal("signals-based module should not serialize")
	}
}
