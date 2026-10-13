package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestIntegerSumMixedWidthSplit(t *testing.T) {
	m := &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I64, Mutable: true}}}}
	// 0x100000001*i has two equal halves for every unsigned i32 counter.
	term := []byte{0x20, 2, 0xad, 0x42, 0x81, 0x80, 0x80, 0x80, 0x10, 0x7e, 0x24, 0, 0x23, 0, 0xa7, 0x23, 0, 0x42, 32, 0x88, 0xa7, 0x6a, 0x0b}
	body := append([]byte{0x40, 0x20, 1, 0x02, 0x7f}, term...)
	body = append(body, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b)
	r := wasm.ReaderFrom(body)
	p, ok := InspectIntegerSumLoopForModule(&r, m, false)
	if !ok || p.Coefficient != 2 || p.Constant != 0 || p.Wide || p.GlobalWriteCount != 1 || p.GlobalWrites[0].Coefficient != 0x100000001 || p.GlobalWrites[0].Constant != 0 || !p.GlobalWrites[0].Wide || r.Offset() != 0 {
		t.Fatalf("plan=%+v ok=%t cursor=%d", p, ok, r.Offset())
	}
}

func TestIntegerMixedWidthRejectsUnsignedCarriesAndEffects(t *testing.T) {
	for name, body := range map[string][]byte{
		"extension-after-wrap":  {0x20, 2, 0x41, 1, 0x6a, 0xad, 0xa7, 0x0b},
		"shift-remainder-carry": {0x20, 2, 0xad, 0x42, 1, 0x88, 0xa7, 0x0b},
		"shift-after-i64-wrap":  {0x20, 2, 0xad, 0x42, 0x82, 0x80, 0x80, 0x80, 0x10, 0x7e, 0x42, 32, 0x88, 0xa7, 0x0b},
		"signed-extension":      {0x20, 2, 0xac, 0xa7, 0x0b},
		"signed-shift":          {0x20, 2, 0xad, 0x42, 32, 0x87, 0xa7, 0x0b},
		"variable-shift":        {0x20, 2, 0xad, 0x20, 2, 0xad, 0x88, 0xa7, 0x0b},
		"other-local":           {0x20, 2, 0xad, 0x20, 3, 0xad, 0x7c, 0xa7, 0x0b},
		"nonlinear":             {0x20, 2, 0xad, 0x20, 2, 0xad, 0x7e, 0xa7, 0x0b},
		"call":                  {0x20, 2, 0x10, 0, 0x0b},
		"memory":                {0x20, 2, 0x28, 2, 0, 0x0b},
		"wrong-width":           {0x20, 2, 0x42, 1, 0x7c, 0xa7, 0x0b},
		"read-before-write":     {0x23, 0, 0xa7, 0x0b},
	} {
		r := wasm.ReaderFrom(body)
		if p, ok := inspectIntegerMixedWidthTerm(&r, nil, 4); ok {
			t.Fatalf("accepted %s: %+v", name, p)
		}
	}
}

func TestIntegerMixedWidthShiftProof(t *testing.T) {
	// Check the admitted affine result against the actual unsigned bit operations
	// at wrap boundaries and a deterministic spread over the full counter domain.
	for _, factor := range []uint64{0, 1, 2, 4, 65536, 0x100000000, 0x100000001, 0x100000002, ^uint64(0)} {
		for _, shift := range []uint64{0, 1, 4, 16, 31, 32, 33, 63, 64, 65} {
			for _, bias := range []uint64{0, 1, 65535, 0x100000000, ^uint64(0)} {
				body := []byte{0x20, 2, 0xad, 0x42}
				body = append(body, wasmtest.SLEB64(int64(factor))...)
				body = append(body, 0x7e, 0x42)
				body = append(body, wasmtest.SLEB64(int64(bias))...)
				body = append(body, 0x7c, 0x42)
				body = append(body, wasmtest.SLEB64(int64(shift))...)
				body = append(body, 0x88, 0xa7, 0x0b)
				r := wasm.ReaderFrom(body)
				p, ok := inspectIntegerMixedWidthTerm(&r, nil, 4)
				if !ok {
					continue
				}
				cases := []uint64{0, 1, 2, 65535, 65536, 0x7fffffff, 0x80000000, 0xfffffffe, 0xffffffff}
				x := uint32(0x7a6d4821)
				for j := 0; j < 64; j++ {
					x = x*1664525 + 1013904223
					cases = append(cases, uint64(x))
				}
				for _, i := range cases {
					want := uint32((factor*i + bias) >> (shift & 63))
					got := uint32(p.coefficient*i + p.constant)
					if got != want {
						t.Fatalf("factor=%x bias=%x shift=%d i=%x got=%x want=%x", factor, bias, shift, i, got, want)
					}
				}
			}
		}
	}
}
