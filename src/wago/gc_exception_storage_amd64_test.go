//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// (module (type (struct (field exnref))) (tag)
//
//	(func (export "run") (result i32)
//	  (block (result exnref)
//	    (try_table (catch_all_ref 0) (throw 0)) (unreachable))
//	  struct.new 0 struct.get 0 0 ref.is_null i32.eqz))
func exceptionStructStorageModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x69, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x01})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x69, 0x1f, 0x40, 0x01, 0x03, 0x00, 0x08, 0x00, 0x0b, 0x00, 0x0b,
			0xfb, 0x00, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xd1, 0x45, 0x0b,
		}))),
	)
}

// Native AMD64 catch references point into the current function's frame. They
// cannot be stored in the collector heap, even if a pointer-sized field were
// used. This module used to compile and fail in run with a reference type
// mismatch for field 0:0 instead of rejecting the unsupported representation.
func TestGCExceptionStructStorage(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), exceptionStructStorageModule())
	if c != nil {
		c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "type 0 field 0: exception reference storage requires owned exception references") {
		t.Fatalf("compile = %v, want exception storage rejection", err)
	}
}
