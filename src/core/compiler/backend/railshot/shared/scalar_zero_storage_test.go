package shared

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestScalarDeclaredZeroStorage(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		types := make([]wasm.ValType, 256)
		widths := make([]bool, 256)
		for i := range types {
			types[i] = wasm.I32
			if mixed && i%2 == 1 {
				types[i] = wasm.I64
				widths[i] = true
			}
		}
		// The last read remains zero; local zero is overwritten after an older
		// zero read is dropped. Both width pools must retain independent ownership.
		body := []byte{0x20, 0, 0x1a, 0x41, 7, 0x21, 0, 0x20, 0xfe, 1, 0x0b}
		ft := &wasm.CompType{Results: []wasm.ValType{wasm.I32}}
		summary := AdmitScalar(body, ft, types)
		if !summary.Eligible {
			t.Fatal("fixture rejected")
		}
		var state ScalarState
		if _, err := state.CompileScalar(body, summary, widths, 0, &scalarTestTarget{}); err != nil {
			t.Fatal(err)
		}
		if len(state.nodes) > 4 || state.Memory() > 1536 {
			t.Fatalf("mixed=%v nodes=%d memory=%d", mixed, len(state.nodes), state.Memory())
		}
		bindings := make(map[scalarID]int32)
		for _, id := range state.locals {
			bindings[id]++
		}
		for id, n := range state.nodes {
			if n.refs != bindings[scalarID(id)] {
				t.Fatalf("value %d references=%d local bindings=%d", id, n.refs, bindings[scalarID(id)])
			}
		}
	}
}
