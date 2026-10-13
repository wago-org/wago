package shared

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func shiftedI32DotBody(ids [6]uint32, lb, rb int32) []byte {
	b := []byte{0x40}
	idx := func(op byte, x uint32) { b = append(b, op); b = append(b, wasmtest.ULEB(x)...) }
	con := func(x int32) { b = append(b, 0x41); b = append(b, wasmtest.SLEB64(int64(x))...) }
	load := func() { b = append(b, 0x28, 2, 7) }
	bias := func(x int32) { con(x); b = append(b, 0x6a) }
	idx(0x20, ids[0])
	load()
	bias(lb)
	idx(0x20, ids[0])
	idx(0x20, ids[1])
	b = append(b, 0x6a)
	idx(0x22, ids[5])
	con(4)
	b = append(b, 0x6a)
	load()
	bias(rb)
	b = append(b, 0x6c)
	idx(0x20, ids[0])
	con(-4)
	b = append(b, 0x6a)
	load()
	bias(lb)
	idx(0x20, ids[5])
	load()
	bias(rb)
	b = append(b, 0x6c)
	idx(0x20, ids[4])
	b = append(b, 0x6a, 0x6a)
	idx(0x21, ids[4])
	idx(0x20, ids[0])
	con(8)
	b = append(b, 0x6a)
	idx(0x21, ids[0])
	idx(0x20, ids[2])
	idx(0x20, ids[3])
	con(-2)
	b = append(b, 0x6a)
	idx(0x22, ids[3])
	b = append(b, 0x47, 0x0d, 0, 0x0b)
	return b
}
func TestShiftedI32DotLoopProof(t *testing.T) {
	m := &wasm.Module{Memories: []wasm.MemType{{}}}
	ids := [6]uint32{0, 1, 2, 3, 4, 5}
	for _, lb := range []int32{0, -128, 2147483647, -2147483648} {
		for _, rb := range []int32{1, -256, 2147483647} {
			b := shiftedI32DotBody(ids, lb, rb)
			r := wasm.ReaderFrom(b)
			p, ok := InspectShiftedI32DotLoop(&r, m)
			if !ok || p.Pointer != 0 || p.Shift != 1 || p.Limit != 2 || p.Counter != 3 || p.Accumulator != 4 || p.RightAddress != 5 || p.Offset != 7 || p.LeftBias != lb || p.RightBias != rb || p.End != len(b) || r.Offset() != 0 {
				t.Fatalf("plan=%+v ok=%t", p, ok)
			}
			for cut := range len(b) {
				r = wasm.ReaderFrom(b[:cut])
				if _, ok := InspectShiftedI32DotLoop(&r, m); ok || r.Offset() != 0 {
					t.Fatalf("truncation %d", cut)
				}
			}
		}
	}
	for _, out := range []int{0, 3, 4, 5} {
		for other := range 6 {
			if other == out {
				continue
			}
			bad := ids
			bad[out] = bad[other]
			r := wasm.ReaderFrom(shiftedI32DotBody(bad, 0, 1))
			if _, ok := InspectShiftedI32DotLoop(&r, m); ok {
				t.Fatalf("alias %d/%d", out, other)
			}
		}
	}
	b := shiftedI32DotBody(ids, -128, -128)
	for name, bad := range map[string][]byte{"call": bytes.Replace(b, []byte{0x6c}, []byte{0x10, 0}, 1), "store": bytes.Replace(b, []byte{0x28, 2, 7}, []byte{0x36, 2, 7}, 1), "offset": bytes.Replace(b, []byte{0x28, 2, 7}, []byte{0x28, 2, 8}, 1), "escape": bytes.Replace(b, []byte{0x0d, 0}, []byte{0x0d, 1}, 1)} {
		r := wasm.ReaderFrom(bad)
		if _, ok := InspectShiftedI32DotLoop(&r, m); ok {
			t.Fatal(name)
		}
	}
	for _, module := range []*wasm.Module{nil, {}, {Memories: []wasm.MemType{{Shared: true}}}, {Memories: []wasm.MemType{{Limits: wasm.Limits{Addr64: true}}}}} {
		r := wasm.ReaderFrom(b)
		if _, ok := InspectShiftedI32DotLoop(&r, module); ok {
			t.Fatal("memory eligibility")
		}
	}
}
