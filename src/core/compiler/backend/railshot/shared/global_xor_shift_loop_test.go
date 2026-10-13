package shared

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func globalXorShiftBody(step, shift int64) []byte {
	b := []byte{0x40, 0x20, 1, 0x02, 0x7f, 0x23, 0, 0x42}
	b = append(b, wasmtest.SLEB64(step)...)
	b = append(b, 0x7c, 0x24, 0, 0x23, 0, 0xa7, 0x23, 0, 0x42)
	b = append(b, wasmtest.SLEB64(shift)...)
	return append(b, 0x88, 0xa7, 0x73, 0x0b, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b)
}

func TestGlobalXorShiftLoopProof(t *testing.T) {
	m := &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I64, Mutable: true}}}}
	for _, step := range []int64{0, 1, -1, 4294967313, -9223372036854775807} {
		for _, shift := range []int64{0, 13, 32, 63, 77, -1} {
			b := globalXorShiftBody(step, shift)
			r := wasm.ReaderFrom(b)
			p, ok := InspectGlobalXorShiftLoop(&r, m)
			if !ok || p.Step != uint64(step) || p.Shift != uint8(uint64(shift)&63) || p.Counter != 2 || p.Accumulator != 1 || p.Bound != 0 || p.Global != 0 || p.End != len(b) || r.Offset() != 0 {
				t.Fatalf("step=%d shift=%d plan=%+v accepted=%t", step, shift, p, ok)
			}
			for cut := range len(b) {
				r = wasm.ReaderFrom(b[:cut])
				if _, ok := InspectGlobalXorShiftLoop(&r, m); ok || r.Offset() != 0 {
					t.Fatalf("accepted cut=%d", cut)
				}
			}
		}
	}
}

func TestGlobalXorShiftLoopProofRejectsEffectsAndAliases(t *testing.T) {
	m := &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I64, Mutable: true}}, {Type: wasm.GlobalType{Type: wasm.I64, Mutable: true}}}}
	b := globalXorShiftBody(4294967313, 32)
	for name, bad := range map[string][]byte{
		"call":          bytes.Replace(b, []byte{0x7c}, []byte{0x10, 0, 0x7c}, 1),
		"memory":        bytes.Replace(b, []byte{0x42}, []byte{0x41, 0, 0x29, 3, 0, 0x42}, 1),
		"trap":          bytes.Replace(b, []byte{0x7c}, []byte{0x7f}, 1),
		"other-global":  bytes.Replace(b, []byte{0x24, 0}, []byte{0x24, 1}, 1),
		"signed-bound":  bytes.Replace(b, []byte{0x49}, []byte{0x48}, 1),
		"other-counter": bytes.Replace(b, []byte{0x21, 2}, []byte{0x21, 1}, 1),
		"escape":        bytes.Replace(b, []byte{0x0d, 0}, []byte{0x0d, 1}, 1),
		"fence":         append([]byte{0x40, 0xfe, 3, 0}, b[1:]...),
	} {
		r := wasm.ReaderFrom(bad)
		if _, ok := InspectGlobalXorShiftLoop(&r, m); ok || r.Offset() != 0 {
			t.Fatalf("accepted %s", name)
		}
	}
	for _, typ := range []wasm.GlobalType{{Type: wasm.I64, Mutable: false}, {Type: wasm.I32, Mutable: true}, {Type: wasm.F64, Mutable: true}} {
		m.Globals[0].Type = typ
		r := wasm.ReaderFrom(b)
		if _, ok := InspectGlobalXorShiftLoop(&r, m); ok {
			t.Fatalf("accepted type %+v", typ)
		}
	}
	m.Globals[0].Type = wasm.GlobalType{Type: wasm.I64, Mutable: true}
	m.Imports = []wasm.Import{{Type: wasm.NewGlobalExternType(m.Globals[0].Type)}}
	r := wasm.ReaderFrom(b)
	if _, ok := InspectGlobalXorShiftLoop(&r, m); ok {
		t.Fatal("accepted imported global")
	}
}
