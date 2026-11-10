//go:build (linux || darwin) && arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestTailLoopInlineColdReturnValues(t *testing.T) {
	if profileEnabled {
		t.Skip("tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	tailLoopInlineEnabled = true
	defer func() { tailLoopInlineEnabled = old }()
	for _, wide := range []bool{false, true} {
		typ, constant, mul, add, lt := wasm.I32, byte(0x41), byte(0x6c), byte(0x6a), byte(0x49)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, constant, mul, add, lt = wasm.I64, 0x42, 0x7e, 0x7c, 0x54
			fold = func(v uint64) uint64 { return v }
		}
		params := []wasm.ValType{wasm.I32, typ, typ}
		results := []wasm.ValType{typ}
		body := []byte{0, 0x20, 0, 0x45, 0x04, 0x40, 0x20, 1, constant, 3, mul, constant, 7, add, 0x0f, 0x0b,
			0x20, 1, 0x20, 2, lt, 0x04, 0x40, 0x20, 1, 0x20, 2, add, 0x0f, 0x0b,
			0x20, 0, 0x41, 1, 0x6b, 0x20, 1, constant, 5, mul, constant, 1, add, 0x20, 2, 0x12, 1, 0x0b}
		m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x0b}}, funcDef{params, results, body})
		for _, input := range [][3]uint64{{0, 17, 5}, {1, 3, 5}, {1, 9, 5}, {31, 31, 17}, {100000, 1, 0}, {3, ^uint64(0), 0}, {7, 0x80000000, 0x80000001}} {
			n, a, b := input[0], fold(input[1]), fold(input[2])
			for n != 0 && a >= b {
				n--
				a = fold(a*5 + 1)
			}
			want := fold(3*a + 7)
			if n != 0 {
				want = fold(a + b)
			}
			opts := CompileOptions{}
			var stats ModuleStats
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runArm64WrapperWithOptions(t, m, opts, input[0]|0xdeadbeef00000000, input[1], input[2])
			if err != nil || got != want {
				t.Fatalf("wide=%t input=%v got=%x,%v want %x", wide, input, got, err, want)
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-cold-return"] != 2 {
				t.Fatalf("cold returns=%d want 2", stats.Funcs[0].Peephole["inline-cold-return"])
			}
		}
	}
}

func TestTailLoopInlineColdReturnDoesNotMoveTraps(t *testing.T) {
	if profileEnabled {
		t.Skip("tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	tailLoopInlineEnabled = true
	defer func() { tailLoopInlineEnabled = old }()
	params, results := []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}
	m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}},
		funcDef{params, results, []byte{0,
			0x20, 0, 0x45, 0x04, 0x40, 0x41, 1, 0x41, 0, 0x6e, 0x0f, 0x0b,
			0x20, 0, 0x41, 1, 0x46, 0x04, 0x40, 0x20, 1, 0x0f, 0x0b,
			0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x12, 1, 0x0b}})
	for _, n := range []uint64{0, 1, 2, 100000} {
		var stats ModuleStats
		opts := CompileOptions{}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		got, err := runArm64WrapperWithOptions(t, m, opts, n, 37)
		if n == 0 {
			var trap *rt.TrapError
			if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
				t.Fatalf("n=%d error=%v want division by zero", n, err)
			}
		} else if err != nil || got != 37 {
			t.Fatalf("n=%d got=%d,%v want 37", n, got, err)
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-cold-return"] != 1 {
			t.Fatal("the trapping return was moved")
		}
	}
}
