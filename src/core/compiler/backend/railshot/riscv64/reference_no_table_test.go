//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

// Exercise the instruction walkers directly: a public validator would reject
// these missing-table bodies before reaching the newly independent ref gate.
func TestReferenceCarriersWithoutTableDoNotEnableTableInstructions(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc}
	globals := []wasm.GlobalType{{Type: wasm.FuncRef}}
	state := moduleState{production: true, importReferences: true, globals: []globalSpec{{typ: wasm.FuncRef, wide: true}}}
	check := func(body []byte, wantError bool) {
		t.Helper()
		fn := &wasm.Func{BodyBytes: body}
		_, scalar := walkInteger(fn, ft, &state, nil)
		_, vector := walkSWARPairsWithImportMode(fn, ft, false, nil, nil, globals, 0, nil, nil, 0, 0, true, false, nil)
		for _, err := range []error{scalar, vector} {
			if wantError && !errors.Is(err, ErrUnsupported) || !wantError && err != nil {
				t.Fatalf("body %x, reject=%t: %v", body, wantError, err)
			}
		}
	}
	check([]byte{0x23, 0, 0xd1, 0x1a, 0x0b}, false)
	for _, body := range [][]byte{
		{0x41, 0, 0x25, 0, 0x1a, 0x0b},
		{0x41, 0, 0xd0, 0x70, 0x26, 0, 0x0b},
		{0xfc, 16, 0, 0x1a, 0x0b},
		{0xd0, 0x70, 0x41, 1, 0xfc, 15, 0, 0x1a, 0x0b},
		{0x41, 0, 0xd0, 0x70, 0x41, 1, 0xfc, 17, 0, 0x0b},
		{0x41, 0, 0x41, 0, 0x41, 0, 0xfc, 14, 0, 0, 0x0b},
		{0x00, 0x25, 0, 0x1a, 0x0b},
		{0x00, 0xfc, 16, 0, 0x1a, 0x0b},
	} {
		check(body, true)
	}
}
