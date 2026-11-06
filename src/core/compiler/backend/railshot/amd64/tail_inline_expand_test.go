//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestInlineTailExpansionPreservesLoopsAndFunctionLabels(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	for _, wide := range []bool{false, true} {
		typ, block, k, add, mul := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x6c)
		if wide {
			typ, block, k, add, mul = wasm.I64, 0x7e, 0x42, 0x7c, 0x7e
		}
		fold := func(n uint64) uint64 {
			if !wide {
				return uint64(uint32(n))
			}
			return n
		}
		params, results := []wasm.ValType{wasm.I32, typ, typ}, []wasm.ValType{typ}
		// A declared local is reset at every logical invocation. br 1 exits the
		// source function from its if, including copies nested inside other ifs.
		body := []byte{1, 1, block, 0x20, 0, 0x45, 0x04, 0x40,
			0x20, 1, k, 3, mul, 0x20, 2, add, 0x20, 3, add, 0x0c, 1,
			0x0b, k, 13, 0x21, 3,
			0x20, 0, 0x41, 1, 0x6b, 0x20, 2, 0x20, 1, k, 7, add, 0x12, 1, 0x0b}
		m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, k, 7, 0x10, 1, 0x0b}}, funcDef{params, results, body})
		for _, enabled := range []bool{false, true} {
			for _, n := range []uint64{0, 1, 2, 3, 4, 31, 511, 100000} {
				a, b := fold(0xfffffffffffffff0), uint64(7)
				aa, bb := a, b
				if !wide {
					aa |= 0xdeadbeef00000000
					bb |= 0xcafebabe00000000
				}
				var stats ModuleStats
				got, err := runAmd64TailInlineWithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"tail-loop-inline": enabled}}, n|0xdeadbeef00000000, aa, bb)
				for range n {
					a, b = b, fold(a+7)
				}
				want := fold(3*a + b)
				if err != nil || got != want {
					t.Fatalf("wide=%t enabled=%t n=%d got=%x err=%v want=%x", wide, enabled, n, got, err, want)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tail-expand"] != 0) != enabled {
					t.Fatal("bounded tail body expansion was not exercised")
				}
			}
		}
	}
}

func TestInlineTailExpansionRealizesDiscardedOperandTraps(t *testing.T) {
	params, results := []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}
	body := []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05,
		0x41, 1, 0x20, 0, 0x41, 1, 0x6b, 0x6e,
		0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x12, 1, 0x0b, 0x0b}
	m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x41, 17, 0x10, 1, 0x0b}}, funcDef{params, results, body})
	for _, enabled := range []bool{false, true} {
		opts := CompileOptions{Optimizations: map[string]bool{"tail-loop-inline": enabled}}
		if got, _, err := runMemAmd64WithOptions(t, m, opts, nil, 0, 17); err != nil || got != 17 {
			t.Fatalf("enabled=%t base got=%d err=%v", enabled, got, err)
		}
		for _, n := range []uint64{1, 2, 3, 31} {
			_, _, err := runMemAmd64WithOptions(t, m, opts, nil, n, 17)
			var trap *runtime.TrapError
			if !errors.As(err, &trap) || trap.Code != runtime.TrapDivZero {
				t.Fatalf("enabled=%t n=%d discarded operand lost its trap: %v", enabled, n, err)
			}
		}
	}
}

func TestInlineTailExpansionBoundsBranchFanout(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	params, results := []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}
	body := []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05,
		0x20, 0, 0x41, 3, 0x70, 0x45, 0x04, 0x7f,
		0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 1, 0x6a, 0x12, 1, 0x05,
		0x20, 0, 0x41, 3, 0x70, 0x41, 1, 0x46, 0x04, 0x7f,
		0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 2, 0x6a, 0x12, 1, 0x05,
		0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 3, 0x6a, 0x12, 1, 0x0b, 0x0b, 0x0b, 0x0b}
	m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x41, 0, 0x10, 1, 0x0b}}, funcDef{params, results, body})
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()
	for _, n := range []uint64{0, 1, 2, 3, 31, 100000} {
		want := uint32(0)
		for i := n; i > 0; i-- {
			want += uint32(i%3 + 1)
		}
		if got := runCompiledAmd64u(t, c, n, 0xfffffff0); got != uint64(want) {
			t.Fatalf("n=%d got=%x want=%x", n, got, want)
		}
	}
	if diagnosticsEnabled {
		count := stats.Funcs[0].Peephole["inline-tail-expand"]
		if count < 1 || int(count)*len(m.Code[1].BodyBytes) > inlineTailExpansionBytes {
			t.Fatalf("unbounded branch expansion: copies=%d body=%d", count, len(m.Code[1].BodyBytes))
		}
	}
}

func TestInlineTailEntrySpecializesConstantBounds(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	params, results := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}
	body := []byte{1, 2, 0x7f,
		0x20, 0, 0x20, 1, 0x4f, 0x04, 0x40, 0x41, 0, 0x0f, 0x0b,
		0x20, 0, 0x20, 1, 0x6a, 0x41, 1, 0x76, 0x21, 3,
		0x20, 3, 0x41, 5, 0x6c, 0x41, 3, 0x6a, 0x21, 4,
		0x20, 4, 0x20, 2, 0x46, 0x04, 0x40, 0x20, 3, 0x41, 1, 0x6a, 0x0f, 0x0b,
		0x20, 4, 0x20, 2, 0x49, 0x04, 0x7f,
		0x20, 3, 0x41, 1, 0x6a, 0x20, 1, 0x20, 2, 0x12, 1, 0x05,
		0x20, 0, 0x20, 3, 0x20, 2, 0x12, 1, 0x0b, 0x0b}
	wrapper := []byte{0, 0x41, 3, 0x41, 0xe1, 0, 0x20, 0, 0x10, 1, 0x0b}
	m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32}, results, wrapper}, funcDef{params, results, body})
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{Workers: 4, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"tail-loop-inline": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		values := []uint32{0, 1, 2, 3, 4, 17, 133, 253, 373, 0x7fffffff, 0x80000000, 0xffffffff}
		for i := uint32(0); i < 600; i++ {
			values = append(values, i)
		}
		for _, target := range values {
			lo, hi, want := uint32(3), uint32(97), uint32(0)
			for lo < hi {
				mid := (lo + hi) >> 1
				value := 5*mid + 3
				if value == target {
					want = mid + 1
					break
				}
				if value < target {
					lo = mid + 1
				} else {
					hi = mid
				}
			}
			if got := runCompiledAmd64u(t, c, uint64(target)|0xdeadbeef00000000); got != uint64(want) {
				t.Fatalf("enabled=%t target=%x got=%x want=%x", enabled, target, got, want)
			}
		}
		if c.CodeImage != nil {
			c.CodeImage.Close()
		}
		if diagnosticsEnabled {
			for _, marker := range []string{"inline-tail-entry-peel", "inline-tail-constant-get", "inline-tail-constant-if", "inline-tail-compare-reuse"} {
				if (stats.Funcs[0].Peephole[marker] != 0) != enabled {
					t.Fatalf("enabled=%t missing specialization %s", enabled, marker)
				}
			}
		}
	}
}

func TestInlineTailEntryForgetsConditionalWrites(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	results := []wasm.ValType{wasm.I32}
	body := []byte{0, 0x20, 0, 0x45, 0x04, 0x40, 0x20, 1, 0x0f, 0x0b,
		0x20, 2, 0x04, 0x40, 0x20, 0, 0x41, 1, 0x71, 0x04, 0x40,
		0x20, 1, 0x41, 3, 0x6a, 0x21, 1, 0x0b, 0x0b,
		0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 5, 0x6a, 0x20, 2, 0x12, 1, 0x0b}
	m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, results, []byte{0, 0x20, 0, 0x41, 7, 0x20, 1, 0x10, 1, 0x0b}}, funcDef{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, results, body})
	for _, n := range []uint64{0, 1, 2, 3, 31, 100000} {
		for _, flag := range []uint64{0, 1} {
			var stats ModuleStats
			got, err := runAmd64TailInlineWithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats)}, n, flag)
			want := uint32(7 + 5*n + 3*flag*((n+1)/2))
			if err != nil || got != uint64(want) {
				t.Fatalf("n=%d flag=%d got=%x err=%v want=%x", n, flag, got, err, want)
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-tail-entry-peel"] == 0 {
				t.Fatal("conditional-write test did not exercise entry facts")
			}
		}
	}
}

func TestInlineTailEntryConstantTypedIfAndUnusedTrap(t *testing.T) {
	results := []wasm.ValType{wasm.I32}
	for _, constant := range []byte{0, 7} {
		body := []byte{0, 0x20, 0, 0x45, 0x04, 0x40, 0x20, 1, 0x0f, 0x0b,
			0x20, 0, 0x20, 1, 0x04, 0x00, // type 0: (param i32) (result i32)
			0x41, 1, 0x6b, 0x05, 0x1a, 0x41, 1, 0x41, 0, 0x6e, 0x0b,
			0x20, 1, 0x12, 1, 0x0b}
		m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32}, results, []byte{0, 0x20, 0, 0x41, constant, 0x10, 1, 0x0b}}, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, results, body})
		for _, n := range []uint64{0, 1, 2, 3, 31, 100000} {
			got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, n)
			if constant == 0 && n > 0 {
				var trap *runtime.TrapError
				if !errors.As(err, &trap) || trap.Code != runtime.TrapDivZero {
					t.Fatalf("n=%d taken trap lost: %v", n, err)
				}
			} else if err != nil || got != uint64(constant) {
				t.Fatalf("constant=%d n=%d got=%d err=%v", constant, n, got, err)
			}
		}
	}
}
