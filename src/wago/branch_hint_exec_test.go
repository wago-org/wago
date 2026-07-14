package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// branchHintReturnModule has a non-empty br_if edge to the implicit function
// label. It exercises the deferred return-edge layout, which must still route
// through the regular function epilogue.
func branchHintReturnModule(withHint bool) []byte {
	body := []byte{
		0x00,       // no locals
		0x41, 0x08, // branch result: 8
		0x20, 0x00, 0x0d, 0x00, // local.get 0; br_if function label
		0x1a, 0x41, 0x09, 0x0b, // false path: drop 8; return 9; end
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
	}
	if withHint {
		name := wasmtest.Name("metadata.code.branch_hint")
		// Function 0, one branch at function-body offset 5, one-byte unlikely payload.
		payload := append(name, 0x01, 0x00, 0x01, 0x05, 0x01, 0x00)
		sections = append(sections, wasmtest.Section(0, payload))
	}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(code)))
	return wasmtest.Module(sections...)
}

func TestBranchHintUnlikelyReturnEdge(t *testing.T) {
	for _, withHint := range []bool{false, true} {
		c, err := Compile(nil, branchHintReturnModule(withHint))
		if err != nil {
			t.Fatalf("Compile(withHint=%t): %v", withHint, err)
		}
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("Instantiate(withHint=%t): %v", withHint, err)
		}
		for _, tc := range []struct {
			in   int32
			want uint64
		}{{0, 9}, {1, 8}} {
			got, err := in.InvokeContext(context.Background(), "f", I32(tc.in))
			if err != nil || len(got) != 1 || got[0] != tc.want {
				in.Close()
				t.Fatalf("Invoke(withHint=%t, %d) = %v, %v; want %d", withHint, tc.in, got, err, tc.want)
			}
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
