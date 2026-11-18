//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBorrowedCompareReadConcreteRegister(t *testing.T) {
	// Keep both loop-invariant parameters pinned and observable after repeatedly
	// materializing the comparison. CMP may read the pinned left parameter
	// directly, while CSET must preserve it.
	body := []byte{1, 1, 0x7f, // one i32 accumulator local
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0, // local.get 0
		0x20, 1, // local.get 1
		0x48,    // i32.lt_s
		0x20, 3, // local.get accumulator
		0x6a,    // i32.add
		0x21, 3, // local.set accumulator
		0x20, 2, // local.get count
		0x41, 1, // i32.const 1
		0x6b,    // i32.sub
		0x22, 2, // local.tee count
		0x0d, 0, // br_if loop
		0x0b,    // end loop
		0x0b,    // end block
		0x20, 3, // local.get accumulator
		0x20, 0, // local.get 0
		0x6a,    // i32.add
		0x20, 1, // local.get 1
		0x6a, // i32.add
		0x0b,
	}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, pair := range [][2]uint64{{0, 1}, {1, 0}, {7, 7}, {0x7fffffff, 0x80000000}, {0xffffffff, 1}} {
		var stats ModuleStats
		opts := CompileOptions{}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		got, err := runArm64WrapperWithOptions(t, m, opts, pair[0], pair[1], 3)
		want := uint32(pair[0]) + uint32(pair[1])
		if int32(pair[0]) < int32(pair[1]) {
			want += 3
		}
		if err != nil || uint32(got) != want {
			t.Fatalf("pair=%x got=%x want=%x err=%v", pair, got, want, err)
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["borrowed-compare-read"] == 0 {
			t.Fatalf("pair=%x compare did not borrow its left operand: %v", pair, stats.Funcs[0].Peephole)
		}
	}
}

func TestBorrowedCompareReadPreservesOldLocal(t *testing.T) {
	// The left local.get observes the old parameter value. The deferred right
	// expression updates that same local before comparison, so it must retain the
	// existing owned-copy path and compare old against new.
	body := []byte{0,
		0x20, 0, // old local.get 0
		0x20, 0, // local.get 0
		0x41, 1, // i32.const 1
		0x6a,    // i32.add
		0x22, 0, // local.tee 0
		0x48,    // i32.lt_s
		0x20, 0, // new local.get 0
		0x6a, // i32.add
		0x0b,
	}
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, x := range []uint64{0, 1, 7, 0x7ffffffe, 0x7fffffff, 0xffffffff} {
		got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, x)
		old, next := int32(x), int32(uint32(x)+1)
		want := uint32(next)
		if old < next {
			want++
		}
		if err != nil || uint32(got) != want {
			t.Fatalf("x=%x got=%x want=%x err=%v", x, got, want, err)
		}
	}
}
