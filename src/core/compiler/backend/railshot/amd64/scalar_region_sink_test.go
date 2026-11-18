//go:build linux && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math"
	"math/bits"
	"testing"
)

func TestScalarRegionTerminalSinkRuntime(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		want uint64
	}{
		{"subtract-rhs-destination", []byte{0x20, 0, 0x20, 1, 0x6b, 0x21, 1, 0x20, 1}, 0xffffffe9},
		{"add-rhs-destination", []byte{0x20, 0, 0x20, 1, 0x6a, 0x21, 1, 0x20, 1}, 0xfffffff7},
		{"self-increment", []byte{0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 1}, 8},
		{"self-decrement", []byte{0x20, 1, 0x41, 1, 0x6b, 0x21, 1, 0x20, 1}, 6},
		{"multiply-power-two", []byte{0x20, 1, 0x41, 8, 0x6c, 0x21, 1, 0x20, 1}, 56},
		{"multiply-one", []byte{0x20, 1, 0x41, 1, 0x6c, 0x21, 1, 0x20, 1}, 7},
		{"add-zero", []byte{0x20, 1, 0x41, 0, 0x6a, 0x21, 1, 0x20, 1}, 7},
		{"set-get-tee", []byte{0x20, 1, 0x41, 3, 0x6a, 0x21, 0, 0x20, 0, 0x20, 0, 0x6a}, 20},
		{"older-read-before-sink-write", []byte{0x20, 0, 0x20, 1, 0x41, 3, 0x6a, 0x21, 0, 0x20, 0, 0x6b}, 0xffffffe6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			off, _ := scalarRegionRunBody(t, tc.body, mtI32, false)
			on, hits := scalarRegionRunBody(t, tc.body, mtI32, true)
			if off != tc.want || on != tc.want {
				t.Fatalf("off=%x on=%x want=%x", off, on, tc.want)
			}
			if !profileEnabled && !regallocCheckEnabled && hits == 0 {
				t.Fatal("no region activation")
			}
		})
	}
}

func TestScalarRegionMixedFloatLocalsAndResults(t *testing.T) {
	scalarRegionEnable(t)
	prefix := []byte{0x00, 0x20, 0, 0x41, 3, 0x6a, 0x21, 2}                      // integer work before float barrier
	floatBarrier := []byte{0x20, 1, 0x44, 0, 0, 0, 0, 0, 0, 0, 0x40, 0xa0, 0x1a} // f64(local1+2),drop
	cases := []struct {
		name   string
		result wasm.ValType
		tail   []byte
		want   uint64
	}{
		{"integer-result", wasm.I32, []byte{0x20, 2, 0x41, 4, 0x6c, 0x0b}, 32},
		{"float-result", wasm.F64, []byte{0x20, 2, 0x41, 4, 0x6c, 0xb7, 0x20, 1, 0xa0, 0x0b}, math.Float64bits(34.5)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := append(append(append([]byte{}, prefix...), floatBarrier...), tc.tail...)
			m := mod1(t, []wasm.ValType{wasm.I32, wasm.F64, wasm.I32}, []wasm.ValType{tc.result}, body)
			for _, on := range []bool{false, true} {
				scalarRegionsEnabled = on
				got := runAmd64u(t, m, 5, math.Float64bits(2.5), 0)
				if got != tc.want {
					t.Fatalf("enabled=%v got=%x want=%x", on, got, tc.want)
				}
			}
			if diagnosticsEnabled && !profileEnabled && !regallocCheckEnabled {
				scalarRegionsEnabled = true
				stats := compileWithStats(t, m, false)
				ops := 0
				for _, f := range stats.Funcs {
					ops += f.Peephole["scalar-region-op"]
				}
				if ops == 0 {
					t.Fatal("mixed float function did not admit any integer region")
				}
			}
		})
	}
}

func TestScalarRegionTeeByteSwapPriority(t *testing.T) {
	scalarRegionEnable(t)
	body := byteSwapTeeBody(0x00ff00ff, 8, 0)
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, x := range []uint32{0, 1, 0x12345678, 0x80000000, 0xffffffff} {
		for _, on := range []bool{false, true} {
			scalarRegionsEnabled = on
			got := uint32(runAmd64u(t, m, uint64(x)))
			if got != bits.ReverseBytes32(x) {
				t.Fatalf("enabled=%v x=%x got=%x", on, x, got)
			}
		}
	}
	if diagnosticsEnabled {
		scalarRegionsEnabled = true
		stats := compileWithStats(t, m, false)
		if stats.Funcs[0].Peephole["i32-bswap-tee"] != 1 {
			t.Fatal("region displaced byte-swap specialization")
		}
	}
}

func TestScalarRegionTerminalSinkAdmission(t *testing.T) {
	for _, tc := range []struct {
		name  string
		op    wOp
		swap  bool
		c     int64
		depth int
		want  bool
	}{
		{"self-add", opAdd, false, 3, 1, true}, {"commutative-rhs-dest", opAdd, true, 3, 1, true}, {"sub-rhs-dest", opSub, true, 3, 1, false}, {"identity", opAdd, false, 0, 1, false}, {"negative-unit-preserve", opAdd, false, -1, 1, false}, {"multiply-preserve", opMul, false, 8, 1, false}, {"older-value", opAdd, false, 3, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := scalarRegionFixture(mtI32)
			f.wasmPC = 900
			left := storage{kind: stLocalReg, typ: mtI32, reg: R12, idx: 0}
			right := storage{kind: stConst, typ: mtI32, cval: tc.c}
			if tc.swap {
				left, right = right, left
			}
			s := scalarRegionState{n: tc.depth, pending: scalarRegionPending{left: left, right: right, op: tc.op, typ: mtI32, pc: 123, index: tc.depth - 1, valid: true}}
			before := s
			codeLen := f.a.Len()
			got := f.scalarRegionTerminalSink(&s, 0)
			if got != tc.want {
				t.Fatalf("admission=%v want=%v", got, tc.want)
			}
			if f.wasmPC != 900 {
				t.Fatal("sink changed caller PC")
			}
			if !got {
				if s != before || f.a.Len() != codeLen {
					t.Fatal("rejected sink mutated state")
				}
			} else if s.pending.valid || s.values[0].kind != stReg || s.values[0].reg != R12 || !s.owned.has(R12) {
				t.Fatal("accepted sink ownership incorrect")
			}
		})
	}
}

func TestScalarRegionI64SinkBoundaries(t *testing.T) {
	for _, c := range []int64{-2147483649, -2147483648, -1, 1, 2147483647, 2147483648} {
		for _, sub := range []bool{false, true} {
			body := append([]byte{0x20, 1}, scalarRegionI64(c)...)
			op := byte(0x7c)
			want := int64(7) + c
			if sub {
				op = 0x7d
				want = 7 - c
			}
			body = append(body, op, 0x21, 1, 0x20, 1)
			off, _ := scalarRegionRunBody(t, body, mtI64, false)
			on, hits := scalarRegionRunBody(t, body, mtI64, true)
			if off != uint64(want) || on != uint64(want) {
				t.Fatalf("constant=%d sub=%v off=%x on=%x want=%x", c, sub, off, on, uint64(want))
			}
			if !profileEnabled && !regallocCheckEnabled && hits == 0 {
				t.Fatal("i64 case inactive")
			}
		}
	}
}
