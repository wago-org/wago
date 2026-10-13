package shared

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func integerHashStoreBody(x, m, mask, bound uint32, shift int32) []byte {
	constant := func(v uint32) []byte { return append([]byte{0x41}, wasmtest.SLEB64(int64(int32(v)))...) }
	expression := func(second bool) []byte {
		b := []byte{0x20, 1}
		if second {
			b = append(b, 0x41, 1, 0x6a)
		}
		b = append(b, constant(x)...)
		b = append(b, 0x73)
		b = append(b, constant(m)...)
		b = append(b, 0x6c, 0x22, 2)
		b = append(b, constant(uint32(shift))...)
		b = append(b, 0x76, 0x20, 2, 0x73)
		b = append(b, constant(mask)...)
		return append(b, 0x71)
	}
	b := []byte{0x40, 0x20, 0}
	b = append(b, expression(false)...)
	b = append(b, 0x36, 2, 7, 0x20, 0, 0x41, 4, 0x6a)
	b = append(b, expression(true)...)
	b = append(b, 0x36, 2, 7, 0x20, 0, 0x41, 8, 0x6a, 0x21, 0, 0x20, 1, 0x41, 2, 0x6a, 0x22, 1)
	b = append(b, constant(bound)...)
	return append(b, 0x47, 0x0d, 0, 0x0b)
}

func TestIntegerHashStoreLoopProof(t *testing.T) {
	module := &wasm.Module{Memories: []wasm.MemType{{}}}
	for _, x := range []uint32{0, 40503, 0xffffffff} {
		for _, mul := range []uint32{0, 2246822507, 0x80000000} {
			for _, mask := range []uint32{255, 0xffffffff} {
				for _, shift := range []int32{0, 13, 32, 77, -1} {
					for _, bound := range []uint32{2, 17, 0xffffffe0} {
						b := integerHashStoreBody(x, mul, mask, bound, shift)
						r := wasm.ReaderFrom(b)
						p, ok := InspectIntegerHashStoreLoop(&r, module)
						if !ok || p.Destination != 0 || p.Seed != 1 || p.Temporary != 2 || p.Xor != x || p.Multiplier != mul || p.Mask != mask || p.Shift != uint8(uint32(shift)&31) || p.Bound != bound || p.Offset != 7 || p.End != len(b) || r.Offset() != 0 {
							t.Fatalf("plan=%+v accepted=%t", p, ok)
						}
						for cut := range len(b) {
							r = wasm.ReaderFrom(b[:cut])
							if _, ok := InspectIntegerHashStoreLoop(&r, module); ok || r.Offset() != 0 {
								t.Fatalf("accepted truncation=%d", cut)
							}
						}
					}
				}
			}
		}
	}
}

func TestIntegerHashStoreLoopProofRejectsEffectsAndAliases(t *testing.T) {
	module := &wasm.Module{Memories: []wasm.MemType{{}}}
	b := integerHashStoreBody(40503, 2246822507, 255, 256, 13)
	for name, bad := range map[string][]byte{
		"call":                 bytes.Replace(b, []byte{0x6c}, []byte{0x10, 0}, 1),
		"load":                 bytes.Replace(b, []byte{0x36, 2, 7}, []byte{0x28, 2, 7}, 1),
		"atomic":               bytes.Replace(b, []byte{0x36, 2, 7}, []byte{0xfe, 0x17, 2, 7}, 1),
		"trap":                 bytes.Replace(b, []byte{0x6c}, []byte{0x6d}, 1),
		"signed-shift":         bytes.Replace(b, []byte{0x76}, []byte{0x75}, 1),
		"escape":               bytes.Replace(b, []byte{0x0d, 0}, []byte{0x0d, 1}, 1),
		"other-offset":         bytes.Replace(b, []byte{0x36, 2, 7}, []byte{0x36, 2, 8}, 1),
		"wrong-stride":         bytes.Replace(b, []byte{0x41, 8, 0x6a}, []byte{0x41, 4, 0x6a}, 1),
		"temporary-seed-alias": bytes.ReplaceAll(bytes.ReplaceAll(b, []byte{0x22, 2}, []byte{0x22, 1}), []byte{0x20, 2}, []byte{0x20, 1}),
		"zero-bound":           integerHashStoreBody(40503, 2246822507, 255, 0, 13),
	} {
		r := wasm.ReaderFrom(bad)
		if _, ok := InspectIntegerHashStoreLoop(&r, module); ok || r.Offset() != 0 {
			t.Fatalf("accepted %s", name)
		}
	}
	for _, bad := range []*wasm.Module{nil, {}, {Memories: []wasm.MemType{{Shared: true}}}, {Memories: []wasm.MemType{{Limits: wasm.Limits{Addr64: true}}}}} {
		r := wasm.ReaderFrom(b)
		if _, ok := InspectIntegerHashStoreLoop(&r, bad); ok {
			t.Fatalf("accepted memory %+v", bad)
		}
	}
}
