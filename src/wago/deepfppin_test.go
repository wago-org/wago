//go:build ((linux && (amd64 || riscv64)) || arm64) && !tinygo

package wago

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// TestDeepFPPinExec runs a call-free function with five f64 params (a+b+c+d+e)
// through the real runtime. On amd64 the fifth hot float local is pinned in an
// extended XMM slot (XMM8); this checks the result is still correct. The
// compile-time pin is asserted in the backend package.
func TestDeepFPPinExec(t *testing.T) {
	f64 := wasm.F64
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{f64, f64, f64, f64, f64}, []wasm.ValType{f64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x20, 0x01, 0xa0, 0x20, 0x02, 0xa0, 0x20, 0x03, 0xa0, 0x20, 0x04, 0xa0, 0x0b,
		}))),
	)
	in, err := Instantiate(MustCompile(mod), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	args := []float64{1.5, 2.25, 3, 4, 5}
	want := 1.5 + 2.25 + 3 + 4 + 5
	uargs := make([]uint64, len(args))
	for i, a := range args {
		uargs[i] = math.Float64bits(a)
	}
	got, err := in.Invoke("f", uargs...)
	if err != nil || len(got) != 1 || math.Float64frombits(got[0]) != want {
		t.Fatalf("f%v = %v (%v), want %v", args, got, err, want)
	}
}
