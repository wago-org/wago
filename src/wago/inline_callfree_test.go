//go:build ((linux && amd64) || arm64) && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// TestInlineCallFreeExec runs a function whose only calls are to a call-free leaf
// (f(x)=add1(add1(x))). After inlining both calls the caller is planned as
// call-free (aggressive pins, STACK_REG off); this checks the result is still
// correct through the real runtime. The compile-time hint itself is asserted in
// the backend package (amd64/inline_callfree_test.go).
func TestInlineCallFreeExec(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x01, 0x10, 0x01, 0x0b}), // f: x; call add1; call add1
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}),       // add1: x+1
		)),
	)
	in, err := Instantiate(MustCompile(mod), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	for _, x := range []uint64{0, 5, 40, 1 << 20} {
		if got, err := in.Invoke("f", x); err != nil || len(got) != 1 || got[0] != x+2 {
			t.Fatalf("f(%d) = %v, %v; want %d", x, got, err, x+2)
		}
	}
}
