//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

// funcDef and modFuncs are construction helpers shared by execution and
// compile-only tests. Keep them available when cross-compiling tests for an
// ARM64 host whose native-code execution harness is unavailable.
type funcDef struct {
	params, results []wasm.ValType
	body            []byte
}

func modFuncs(t testing.TB, fns ...funcDef) *wasm.Module {
	t.Helper()
	var types, funcs, codes [][]byte
	for i, fn := range fns {
		types = append(types, wasmtest.FuncType(fn.params, fn.results))
		funcs = append(funcs, wasmtest.ULEB(uint32(i)))
		codes = append(codes, append(wasmtest.ULEB(uint32(len(fn.body))), fn.body...))
	}
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func simdOp(sub uint32) []byte {
	return append([]byte{0xfd}, wasmtest.ULEB(sub)...)
}
