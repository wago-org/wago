//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

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

func TestTailArgumentSelectPermutationsAndLanes(t *testing.T) {
	if profileEnabled {
		t.Skip("experimental tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	tailLoopInlineEnabled = true
	defer func() { tailLoopInlineEnabled = old }()
	for _, wide := range []bool{false, true} {
		fold := func(v uint64) uint64 {
			if wide {
				return v
			}
			return uint64(uint32(v))
		}
		m := tailSelectFixture(t, wide, false)
		for _, n := range []uint64{0, 1, 2, 3, 31, 511, 100000} {
			a, b, c := fold(0xfffffffffffffff0), fold(0x80000000cafebabe), fold(0xdeadbeef12345678)
			aa, bb, cc := a, b, c
			if !wide {
				aa |= 0x1234567800000000
				bb |= 0xabcdef0000000000
				cc |= 0xdeadbeef00000000
			}
			opts := CompileOptions{}
			var stats ModuleStats
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runArm64WrapperWithOptions(t, m, opts, n|0xdeadbeef00000000, aa, bb, cc)
			for range n {
				if a < b {
					a, b = b, fold(a+7)
				} else {
					a, b = fold(a+b), a^b
				}
			}
			want := fold(2*a + 3*b + 5*c)
			if err != nil || got != want {
				t.Fatalf("wide=%t n=%d got=%x,%v want %x", wide, n, got, err, want)
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["tail-branch-select"] == 0 {
				t.Fatal("conditional tail arguments were not selected")
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["tail-branch-select-flags"] == 0 {
				t.Fatal("tail argument selects rematerialized their condition")
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["tail-select-parameter-home"] == 0 {
				t.Fatal("independent selected argument did not use its parameter home")
			}
		}
	}
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
		got, err := runArm64WrapperWithOptions(t, tailSelectFixture(t, wide, true), opts, 31, 100, 7, 11)
		if err != nil || got != 276 {
			t.Fatalf("wide=%t got=%d,%v want 276", wide, got, err)
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["tail-branch-select"] != 0 {
			t.Fatal("trapping branch was speculated")
		}
	}
}
