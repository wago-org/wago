//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func teeSpillModule(t *testing.T, typ wasm.ValType, overwrite bool) *wasm.Module {
	t.Helper()
	const n = 20
	body := []byte{0x01, n, wasm.MustEncodeValType(typ)} // twenty declared scalar locals
	constOp, addOp := byte(0x41), byte(0x6a)
	if typ == wasm.I64 {
		constOp, addOp = 0x42, 0x7c
	}
	for i := 0; i < n; i++ {
		value := i + 1
		if i == 0 && overwrite {
			value = 7
		}
		body = append(body, constOp, byte(value), 0x22, byte(i)) // const; local.tee i
		if i == 0 && overwrite {
			// The old tee result remains on the operand stack while local 0 changes.
			// Later pressure must spill the old value to a distinct slot, not reuse
			// local 0's newly-written canonical home.
			body = append(body, constOp, 9, 0x21, 0x00)
		}
	}
	for i := 1; i < n; i++ {
		body = append(body, addOp)
	}
	body = append(body, 0x0b)
	return mod1(t, nil, []wasm.ValType{typ}, body)
}

func TestTeeSpillElidesRedundantFrameCopies(t *testing.T) {
	saved := teeSpillElideEnabled
	defer func() { teeSpillElideEnabled = saved }()

	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
		m := teeSpillModule(t, typ, false)
		teeSpillElideEnabled = false
		off := compileWithStats(t, m, false).Funcs[0]
		teeSpillElideEnabled = true
		on := compileWithStats(t, m, false).Funcs[0]

		if got := on.Peephole["tee-spill-elide"]; got == 0 {
			t.Fatalf("%v: tee-spill-elide = 0 (all: %v)", typ, on.Peephole)
		}
		if on.Spills >= off.Spills {
			t.Fatalf("%v: enabled spills = %d, disabled = %d", typ, on.Spills, off.Spills)
		}
		if on.FrameBytes >= off.FrameBytes {
			t.Fatalf("%v: enabled frame = %d bytes, disabled = %d", typ, on.FrameBytes, off.FrameBytes)
		}
	}
}

func TestTeeSpillAliasInvalidatedByOverwrite(t *testing.T) {
	saved := teeSpillElideEnabled
	defer func() { teeSpillElideEnabled = saved }()
	teeSpillElideEnabled = true

	if got := runAmd64(t, teeSpillModule(t, wasm.I32, true)); got != 216 {
		t.Fatalf("i32 overwritten tee result = %d, want 216", got)
	}
	if got := runAmd64u(t, teeSpillModule(t, wasm.I64, true)); got != 216 {
		t.Fatalf("i64 overwritten tee result = %d, want 216", got)
	}
}
