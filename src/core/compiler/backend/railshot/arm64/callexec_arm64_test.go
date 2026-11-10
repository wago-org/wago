// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestCallExec compiles a 2-function module where f(x) = g(x) + 1 and g(x) = 2x,
// then executes f through its runtime wrapper — exercising an
// inter-function call (BL relocation + reg-ABI arg/result passing).
func TestCallExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		// func 0 (f): local.get 0; call 1; i32.const 1; i32.add
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x41, 0x01, 0x6a, 0x0b}},
		// func 1 (g): local.get 0; local.get 0; i32.add  (= 2x)
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x20, 0x00, 0x6a, 0x0b}},
	)
	for _, x := range []uint64{5, 0, 100} {
		if got := runArm64u(t, m, x); uint32(got) != uint32(2*x+1) {
			t.Fatalf("f(%d) = %d, want %d", x, uint32(got), 2*x+1)
		}
	}
}

func TestLeafCallResultStaysInX0(t *testing.T) {
	oldInline := inlineEnabled
	inlineEnabled = false
	t.Cleanup(func() { inlineEnabled = oldInline })
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		// The result feeds arithmetic, rather than local.set fusion, so it should
		// remain allocator-owned in X0 after the pin-preserving leaf returns.
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x41, 0x01, 0x6a, 0x0b}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x41, 0x02, 0x6c, 0x0b}},
	)
	s := compileWithStats(t, m, false).Funcs[0]
	if got := s.Peephole["call-result-x0"]; got != 1 {
		t.Fatalf("call-result-x0 = %d, want 1 (all: %v)", got, s.Peephole)
	}
}

func TestMixedCallWrapperArgsExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		// f(x) = trunc(g(float(x), x)); both call operands are register-resident.
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0xb7, 0x20, 0x00, 0x10, 0x01, 0xaa, 0x0b}},
		// g(a, b) = a + float(b).
		funcDef{[]wasm.ValType{wasm.F64, wasm.I32}, []wasm.ValType{wasm.F64}, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0xb7, 0xa0, 0x0b}},
	)
	stats := compileWithStats(t, m, false).Funcs[0]
	if got := stats.Calls["mixed"]; got != 1 {
		t.Fatalf("mixed register calls = %d, want 1 (all: %v)", got, stats.Calls)
	}
	for _, x := range []uint64{0, 1, 7, 100} {
		if got := runArm64u(t, m, x); uint32(got) != uint32(2*x) {
			t.Fatalf("f(%d) = %d, want %d", x, uint32(got), 2*x)
		}
	}
}

func TestMixedCallPreservesBelowOperandsAndConstants(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		// Keep x below the two call arguments. The f64 constant is loaded directly
		// into V0 while the below operand is canonicalized independently.
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x14, 0x40, 0x41, 0x03, 0x10, 0x01, 0xaa, 0x6a, 0x0b}},
		funcDef{[]wasm.ValType{wasm.F64, wasm.I32}, []wasm.ValType{wasm.F64}, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0xb7, 0xa0, 0x0b}},
	)
	// Call-bearing internal entries read the stack fence from the module context.
	// Enter through the runtime wrapper so X26 is initialized rather than inherited
	// from Go's closure register, whose value varies with the preceding tests.
	opts := CompileOptions{Optimizations: map[string]bool{"stack-fence": true}}
	for _, x := range []uint64{0, 1, 17} {
		got, err := runArm64WrapperWithOptions(t, m, opts, x)
		if err != nil || uint32(got) != uint32(x+8) {
			t.Fatalf("f(%d) = %d, %v; want %d", x, uint32(got), err, x+8)
		}
	}
}

func TestTwoIntegerResultRegisterCall(t *testing.T) {
	oldInline := inlineEnabled
	inlineEnabled = false
	t.Cleanup(func() { inlineEnabled = oldInline })
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		// f(x) = sum(g(x)); g returns x and x+1 in X0/X1.
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x6a, 0x0b}},
		funcDef{i32, []wasm.ValType{wasm.I32, wasm.I32}, []byte{0x00, 0x20, 0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
	)
	stats := compileWithStats(t, m, false).Funcs[0]
	if got := stats.Calls["regabi"]; got != 1 {
		t.Fatalf("regabi calls = %d, want 1 (all: %v)", got, stats.Calls)
	}
	for _, x := range []uint64{0, 1, 7, 100} {
		if got := runArm64u(t, m, x); uint32(got) != uint32(2*x+1) {
			t.Fatalf("f(%d) = %d, want %d", x, uint32(got), 2*x+1)
		}
	}
}
