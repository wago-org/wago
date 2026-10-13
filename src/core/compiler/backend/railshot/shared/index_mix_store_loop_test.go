package shared

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestIndexMixStoreLoopProof(t *testing.T) {
	m := &wasm.Module{Memories: []wasm.MemType{{}}}
	for _, add := range []uint32{0, 0x9e3779b9, 0xffffffff} {
		for _, shift := range []int32{0, 16, 33, -1} {
			for _, boundFirst := range []bool{false, true} {
				constant := uint32(0)
				for _, bound := range []*uint32{nil, &constant} {
					b := wasmtest.IndexMixStoreLoopBody(add, 0x85ebca6b, 65535, 7, shift, 77, boundFirst, bound)
					r := wasm.ReaderFrom(b)
					p, ok := InspectIndexMixStoreLoop(&r, m)
					wantBound := uint32(3)
					if bound != nil {
						wantBound = *bound
					}
					if !ok || p.Destination != 0 || p.Seed != 1 || p.Temporary != 2 || p.Bound != wantBound || p.BoundConstant != (bound != nil) || p.Add != add || p.Multiplier != 0x85ebca6b || p.Mask != 65535 || p.Offset != 7 || p.FirstShift != uint8(uint32(shift)&31) || p.LastShift != 13 || p.End != len(b) || r.Offset() != 0 {
						t.Fatalf("accepted%t plan%+v", ok, p)
					}
					for cut := range len(b) {
						r = wasm.ReaderFrom(b[:cut])
						if _, ok := InspectIndexMixStoreLoop(&r, m); ok || r.Offset() != 0 {
							t.Fatalf("accepted truncation%d", cut)
						}
					}
				}
			}
		}
	}
}

func TestIndexMixStoreLoopProofRejectsChanges(t *testing.T) {
	m := &wasm.Module{Memories: []wasm.MemType{{}}}
	b := wasmtest.IndexMixStoreLoopBody(0x9e3779b9, 0x85ebca6b, 65535, 7, 16, 13, true, nil)
	for name, bad := range map[string][]byte{
		"signed-shift":         bytes.Replace(b, []byte{0x76}, []byte{0x75}, 1),
		"call":                 bytes.Replace(b, []byte{0x6c}, []byte{0x10, 0}, 1),
		"division":             bytes.Replace(b, []byte{0x6c}, []byte{0x6d}, 1),
		"load":                 bytes.Replace(b, []byte{0x36, 2, 7}, []byte{0x28, 2, 7}, 1),
		"offset":               bytes.Replace(b, []byte{0x36, 2, 7}, []byte{0x36, 2, 8}, 1),
		"alignment":            bytes.Replace(b, []byte{0x36, 2, 7}, []byte{0x36, 3, 7}, 1),
		"stride":               bytes.Replace(b, []byte{0x41, 8, 0x6a}, []byte{0x41, 4, 0x6a}, 1),
		"escape":               bytes.Replace(b, []byte{0x0d, 0}, []byte{0x0d, 1}, 1),
		"bound-alias":          bytes.Replace(b, []byte{0x20, 3}, []byte{0x20, 2}, 1),
		"temporary-seed-alias": bytes.ReplaceAll(bytes.ReplaceAll(b, []byte{0x22, 2}, []byte{0x22, 1}), []byte{0x20, 2}, []byte{0x20, 1}),
	} {
		r := wasm.ReaderFrom(bad)
		if _, ok := InspectIndexMixStoreLoop(&r, m); ok || r.Offset() != 0 {
			t.Fatalf("accepted %s", name)
		}
	}
	for _, bad := range []*wasm.Module{nil, {}, {Memories: []wasm.MemType{{Shared: true}}}, {Memories: []wasm.MemType{{Limits: wasm.Limits{Addr64: true}}}}} {
		r := wasm.ReaderFrom(b)
		if _, ok := InspectIndexMixStoreLoop(&r, bad); ok {
			t.Fatalf("accepted memory %+v", bad)
		}
	}
}
