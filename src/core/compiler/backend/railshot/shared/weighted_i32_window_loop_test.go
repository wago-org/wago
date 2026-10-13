package shared

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func weightedI32WindowBody(n int, bias, divisor, multiplier int32) []byte {
	b := []byte{0x40}
	get := func(x uint32) { b = append(b, 0x20); b = append(b, wasmtest.ULEB(x)...) }
	con := func(x int32) { b = append(b, 0x41); b = append(b, wasmtest.SLEB64(int64(x))...) }
	for i := 0; i < n; i++ {
		get(uint32(4 + i))
		get(0)
		off := int32((n - i - 1) * 4)
		if off != 0 {
			con(off)
			b = append(b, 0x6a)
			if off == 4 {
				b = append(b, 0x22, 1)
			}
		}
		b = append(b, 0x28, 2, 7)
		con(bias)
		b = append(b, 0x6a, 0x6c)
	}
	for i := 1; i < n; i++ {
		b = append(b, 0x6a)
	}
	con(divisor)
	b = append(b, 0x6d)
	get(3)
	b = append(b, 0x73)
	con(multiplier)
	b = append(b, 0x6c, 0x21, 3)
	get(1)
	b = append(b, 0x21, 0)
	get(2)
	con(-1)
	b = append(b, 0x6a, 0x22, 2, 0x0d, 0, 0x0b)
	return b
}
func TestWeightedI32WindowLoopProof(t *testing.T) {
	m := &wasm.Module{Memories: []wasm.MemType{{}}}
	for _, n := range []int{4, 8, 12, 16, 20, 24, 28, 32} {
		for _, bias := range []int32{0, -32768, -2147483648, 2147483647} {
			for _, div := range []int32{1, 2, 128, 1 << 30} {
				b := weightedI32WindowBody(n, bias, div, -17)
				r := wasm.ReaderFrom(b)
				p, ok := InspectWeightedI32WindowLoop(&r, m)
				if !ok || p.Terms != n || p.Pointer != 0 || p.NextPointer != 1 || p.Counter != 2 || p.Hash != 3 || p.Bias != bias || p.Divisor != uint32(div) || p.Multiplier != -17 || p.Offset != 7 || p.End != len(b) || r.Offset() != 0 {
					t.Fatalf("plan=%+v ok=%t", p, ok)
				}
				for i, c := range p.Coefficients[:n] {
					if c != uint32(4+i) {
						t.Fatal("coefficient order")
					}
				}
				for cut := range len(b) {
					r = wasm.ReaderFrom(b[:cut])
					if _, ok := InspectWeightedI32WindowLoop(&r, m); ok || r.Offset() != 0 {
						t.Fatalf("truncation=%d", cut)
					}
				}
			}
		}
	}
	b := weightedI32WindowBody(8, -32768, 128, 16777619)
	for name, bad := range map[string][]byte{"call": bytes.Replace(b, []byte{0x6c}, []byte{0x10, 0}, 1), "store": bytes.Replace(b, []byte{0x28, 2, 7}, []byte{0x36, 2, 7}, 1), "offset": bytes.Replace(b, []byte{0x28, 2, 7}, []byte{0x28, 2, 8}, 1), "alias-weight": bytes.Replace(b, []byte{0x20, 4}, []byte{0x20, 0}, 1), "escape": bytes.Replace(b, []byte{0x0d, 0}, []byte{0x0d, 1}, 1), "alias-output": bytes.Replace(b, []byte{0x22, 1}, []byte{0x22, 0}, 1)} {
		r := wasm.ReaderFrom(bad)
		if _, ok := InspectWeightedI32WindowLoop(&r, m); ok {
			t.Fatal(name)
		}
	}
	for _, d := range []int32{0, -1, -128, 3, 127} {
		r := wasm.ReaderFrom(weightedI32WindowBody(8, 0, d, 1))
		if _, ok := InspectWeightedI32WindowLoop(&r, m); ok {
			t.Fatalf("divisor%d", d)
		}
	}
	for _, module := range []*wasm.Module{nil, {}, {Memories: []wasm.MemType{{Shared: true}}}, {Memories: []wasm.MemType{{Limits: wasm.Limits{Addr64: true}}}}} {
		r := wasm.ReaderFrom(b)
		if _, ok := InspectWeightedI32WindowLoop(&r, module); ok {
			t.Fatal("memory eligibility")
		}
	}
}
