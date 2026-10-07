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

// Null carriers authorize neither a descriptor load nor a different reference
// class, including in unreachable code. Private pilots retain their own gate.
func TestNullReferenceCarrierInstructionGates(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc}
	state := moduleState{production: true, importReferences: true, functions: []functionSpec{{}}}
	functions := []*wasm.CompType{ft}
	valid := []byte{0xd0, 0x70, 0xd1, 0x1a, 0x0b}
	fn := &wasm.Func{BodyBytes: valid}
	if _, err := walkInteger(fn, ft, &state, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := walkSWARPairsWithImportMode(fn, ft, false, nil, functions, nil, 0, nil, nil, 0, 0, true, false, nil); err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{
		{0xd2, 0, 0x1a, 0x0b}, // Existing function index without descriptor state.
		{0x00, 0xd2, 0, 0x1a, 0x0b},
		{0xd0, 0x6f, 0x1a, 0x0b},
		{0x00, 0xd0, 0x6f, 0x1a, 0x0b},
		{0x42, 0, 0xd1, 0x1a, 0x0b},
		{0xd0, 0x70, 0x50, 0x1a, 0x0b},
		{0x00, 0x25, 0, 0x1a, 0x0b},
		{0xfc, 16, 0, 0x1a, 0x0b},
		{0x41, 0, 0xd0, 0x70, 0x26, 0, 0x0b},
		{0xd0, 0x70, 0xd0, 0x70, 0x41, 0, 0x1c, 1, 0x7e, 0x1a, 0x0b},
	} {
		fn.BodyBytes = body
		_, a := walkInteger(fn, ft, &state, nil)
		_, b := walkSWARPairsWithImportMode(fn, ft, false, nil, functions, nil, 0, nil, nil, 0, 0, true, false, nil)
		if !errors.Is(a, ErrUnsupported) || !errors.Is(b, ErrUnsupported) {
			t.Fatalf("body %x: scalar %v, SWAR %v", body, a, b)
		}
	}
	fn.BodyBytes = valid
	state.importReferences = false
	_, a := walkInteger(fn, ft, &state, nil)
	_, b := walkSWARPairsWithImportMode(fn, ft, false, nil, functions, nil, 0, nil, nil, 0, 0, false, false, nil)
	if !errors.Is(a, ErrUnsupported) || !errors.Is(b, ErrUnsupported) {
		t.Fatal("private pilot widened", a, b)
	}
}
