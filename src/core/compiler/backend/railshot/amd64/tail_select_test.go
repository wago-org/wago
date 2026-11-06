//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestTailArgumentSelectIndependentHomes(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	for _, wide := range []bool{false, true} {
		typ, block, constant, eqz, sub, add, mul, lt := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x6b), byte(0x6a), byte(0x6c), byte(0x49)
		if wide {
			typ, block, constant, eqz, sub, add, mul, lt = wasm.I64, 0x7e, 0x42, 0x50, 0x7d, 0x7c, 0x7e, 0x54
		}
		params, results := []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}
		body := []byte{0, 0x20, 0, eqz, 0x04, block, 0x20, 1, constant, 3, mul, 0x20, 2, constant, 5, mul, add, 0x05, 0x20, 1, 0x20, 2, lt, 0x04, block,
			0x20, 0, constant, 1, sub, 0x20, 1, constant, 1, add, 0x20, 2, 0x12, 1, 0x05,
			0x20, 0, constant, 1, sub, 0x20, 1, 0x20, 2, constant, 1, add, 0x12, 1, 0x0b, 0x0b, 0x0b}
		m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x0b}}, funcDef{params, results, body})
		fold := func(n uint64) uint64 {
			if !wide {
				return uint64(uint32(n))
			}
			return n
		}
		for _, enabled := range []bool{false, true} {
			for _, n := range []uint64{0, 1, 2, 31, 511, 100000} {
				for _, initial := range [][2]uint64{{7, 11}, {11, 7}, {0xffffffff, 0xfffffffe}, {0x80000000cafebabe, 0xfffffffffffffff0}} {
					a, b := fold(initial[0]), fold(initial[1])
					aa, bb := a, b
					if !wide {
						aa |= 0xdeadbeef00000000
						bb |= 0xcafebabe00000000
					}
					var stats ModuleStats
					opts := CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"branch-fold": enabled}}
					got, err := runAmd64TailInlineWithOptions(t, m, opts, n, aa, bb)
					for range n {
						if a < b {
							a = fold(a + 1)
						} else {
							b = fold(b + 1)
						}
					}
					want := fold(3*a + 5*b)
					if err != nil || got != want {
						t.Fatalf("wide=%t enabled=%t n=%d got=%x err=%v want=%x", wide, enabled, n, got, err, want)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["tail-branch-select-flags"] != 0) != enabled {
						t.Fatal("tail argument selects did not use the original condition flags")
					}
				}
			}
		}
	}
}

func tailSelectFixture(t testing.TB, wide, trapArm bool) *wasm.Module {
	typ, block, constant, mul, add, xor, lt := wasm.I32, byte(0x7f), byte(0x41), byte(0x6c), byte(0x6a), byte(0x73), byte(0x49)
	if wide {
		typ, block, constant, mul, add, xor, lt = wasm.I64, 0x7e, 0x42, 0x7e, 0x7c, 0x85, 0x54
	}
	params := []wasm.ValType{wasm.I32, typ, typ, typ}
	results := []wasm.ValType{typ}
	body := []byte{0, 0x20, 0, 0x45, 0x04, block, 0x20, 1, constant, 2, mul, 0x20, 2, constant, 3, mul, add, 0x20, 3, constant, 5, mul, add,
		0x05, 0x20, 1, 0x20, 2, lt, 0x04, block, 0x20, 0, 0x41, 1, 0x6b}
	if trapArm {
		div := byte(0x6e)
		if wide {
			div = 0x80
		}
		body = append(body, 0x20, 1, constant, 0, div, 0x20, 2)
	} else {
		body = append(body, 0x20, 2, 0x20, 1, constant, 7, add)
	}
	body = append(body, 0x20, 3, 0x12, 1, 0x05, 0x20, 0, 0x41, 1, 0x6b)
	if trapArm {
		body = append(body, 0x20, 1, 0x20, 2)
	} else {
		body = append(body, 0x20, 1, 0x20, 2, add, 0x20, 1, 0x20, 2, xor)
	}
	body = append(body, 0x20, 3, 0x12, 1, 0x0b, 0x0b, 0x0b)
	return modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x10, 1, 0x0b}}, funcDef{params, results, body})
}

func TestTailArgumentSelectDoesNotSpeculateTraps(t *testing.T) {
	if profileEnabled {
		t.Skip("experimental tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	tailLoopInlineEnabled = true
	defer func() { tailLoopInlineEnabled = old }()
	for _, wide := range []bool{false, true} {
		opts := CompileOptions{}
		var stats ModuleStats
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		got, err := runAmd64TailInlineWithOptions(t, tailSelectFixture(t, wide, true), opts, 31, 100, 7, 11)
		if err != nil || got != 276 {
			t.Fatalf("wide=%t got=%d,%v want 276", wide, got, err)
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["tail-branch-select"] != 0 {
			t.Fatal("trapping branch was speculated")
		}
	}
}

func TestTailArgumentSelectPermutations(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	for _, wide := range []bool{false, true} {
		typ, block, k, eqz, sub, add, mul, xor, lt := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x6b), byte(0x6a), byte(0x6c), byte(0x73), byte(0x49)
		if wide {
			typ, block, k, eqz, sub, add, mul, xor, lt = wasm.I64, 0x7e, 0x42, 0x50, 0x7d, 0x7c, 0x7e, 0x85, 0x54
		}
		params, results := []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}
		body := []byte{0, 0x20, 0, eqz, 0x04, block, 0x20, 1, k, 2, mul, 0x20, 2, k, 3, mul, add, 0x05,
			0x20, 1, 0x20, 2, lt, 0x04, block, 0x20, 0, k, 1, sub, 0x20, 2, 0x20, 1, k, 7, add, 0x12, 1, 0x05,
			0x20, 0, k, 1, sub, 0x20, 1, 0x20, 2, add, 0x20, 1, 0x20, 2, xor, 0x12, 1, 0x0b, 0x0b, 0x0b}
		m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x0b}}, funcDef{params, results, body})
		fold := func(n uint64) uint64 {
			if !wide {
				return uint64(uint32(n))
			}
			return n
		}
		for _, n := range []uint64{0, 1, 2, 31, 511, 100000} {
			a, b := fold(0xfffffffffffffff0), fold(0x80000000cafebabe)
			aa, bb := a, b
			if !wide {
				aa |= 0xdeadbeef00000000
				bb |= 0xcafebabe00000000
			}
			var stats ModuleStats
			got, err := runAmd64TailInlineWithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats)}, n, aa, bb)
			for range n {
				if a < b {
					a, b = b, fold(a+7)
				} else {
					a, b = fold(a+b), a^b
				}
			}
			want := fold(2*a + 3*b)
			if err != nil || got != want {
				t.Fatalf("wide=%t n=%d got=%x err=%v want=%x", wide, n, got, err, want)
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["tail-branch-select-flags"] == 0 {
				t.Fatal("permuted arguments did not exercise conditional selection")
			}
		}
	}
}

func TestTailArgumentSelectRejectsImpurePackets(t *testing.T) {
	f := fn{traceFuncIdx: 1}
	types := []machineType{mtI32, mtI32}
	for _, body := range [][]byte{
		{0x20, 0, 0x41, 0, 0x6e, 0x12, 1, 0x05},    // division may trap
		{0x20, 0, 0x21, 1, 0x20, 1, 0x12, 1, 0x05}, // local write
		{0x10, 0, 0x12, 1, 0x05},                   // ordinary call
		{0x23, 0, 0x12, 1, 0x05},                   // global read
		{0x20, 0, 0x28, 2, 0, 0x12, 1, 0x05},       // memory read
		{0x20, 0, 0x12, 0, 0x05},                   // other tail target
		{0x20, 0, 0x12, 1, 0x0b},                   // wrong ending
		{0x20, 0x80},                               // truncated local index
	} {
		r := wasm.ReaderFrom(body)
		if _, ok := f.scanPureIntegerPacket(&r, types, types[:1], 0x12, 0x05); ok {
			t.Fatalf("accepted impure or malformed packet %x", body)
		}
	}
}
