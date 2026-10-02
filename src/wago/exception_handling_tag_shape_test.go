//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Code generation checks tag payloads only where a tag is thrown or caught, so
// a declared but unused tag wider than the staged directory compiled and then
// failed instance validation with "compiled metadata invalid". Compilation
// must reject it with the bounded-EH limit instead.
func TestUnusedWideTagRejectedAtCompile(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
	c, err := compileStagedExceptionHandlingFeaturesForTest(data, false)
	if err == nil {
		c.Close()
		t.Fatal("compile succeeded; want bounded tag-parameter rejection")
	}
	if !strings.Contains(err.Error(), "at most 2 tag parameters") {
		t.Fatalf("compile error = %v, want bounded tag-parameter rejection", err)
	}
}
