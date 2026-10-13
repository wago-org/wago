package shared

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func pairedI32DotBody(leftBias, rightBias, bound int32) []byte {
	b := []byte{0x40, 0x20, 0, 0x20, 2, 0x6a, 0x22, 4, 0x41, 4, 0x6a, 0x28, 2, 7, 0x41}
	b = append(b, wasmtest.SLEB64(int64(leftBias))...)
	b = append(b, 0x6a, 0x20, 1, 0x20, 2, 0x6a, 0x22, 5, 0x41, 4, 0x6a, 0x28, 2, 7, 0x41)
	b = append(b, wasmtest.SLEB64(int64(rightBias))...)
	b = append(b, 0x6a, 0x6c, 0x20, 4, 0x28, 2, 7, 0x41)
	b = append(b, wasmtest.SLEB64(int64(leftBias))...)
	b = append(b, 0x6a, 0x20, 5, 0x28, 2, 7, 0x41)
	b = append(b, wasmtest.SLEB64(int64(rightBias))...)
	b = append(b, 0x6a, 0x6c, 0x20, 3, 0x6a, 0x6a, 0x21, 3, 0x20, 2, 0x41, 8, 0x6a, 0x22, 2, 0x41)
	b = append(b, wasmtest.SLEB64(int64(bound))...)
	return append(b, 0x47, 0x0d, 0, 0x0b)
}

func TestPairedI32DotLoopProof(t *testing.T) {
	m := &wasm.Module{Memories: []wasm.MemType{{}}}
	for _, left := range []int32{0, -128, 2147483647, -2147483648} {
		for _, right := range []int32{1, -128, -2147483648} {
			for _, bound := range []int32{8, 16, 24, 256, 2147483640} {
				b := pairedI32DotBody(left, right, bound)
				r := wasm.ReaderFrom(b)
				p, ok := InspectPairedI32DotLoop(&r, m)
				if !ok || p.Left != 0 || p.Right != 1 || p.Counter != 2 || p.Accumulator != 3 || p.LeftAddress != 4 || p.RightAddress != 5 || p.LeftBias != left || p.RightBias != right || p.Bound != uint32(bound) || p.Offset != 7 || p.End != len(b) || r.Offset() != 0 {
					t.Fatalf("plan=%+v ok=%t", p, ok)
				}
				for cut := range len(b) {
					r = wasm.ReaderFrom(b[:cut])
					if _, ok := InspectPairedI32DotLoop(&r, m); ok || r.Offset() != 0 {
						t.Fatalf("accepted truncation=%d", cut)
					}
				}
			}
		}
	}
}

func TestPairedI32DotLoopProofRejectsEffectsAndAliases(t *testing.T) {
	m := &wasm.Module{Memories: []wasm.MemType{{}}}
	b := pairedI32DotBody(-128, -128, 256)
	for name, bad := range map[string][]byte{
		"call":                   bytes.Replace(b, []byte{0x6c}, []byte{0x10, 0}, 1),
		"store":                  bytes.Replace(b, []byte{0x28, 2, 7}, []byte{0x36, 2, 7}, 1),
		"atomic":                 bytes.Replace(b, []byte{0x28, 2, 7}, []byte{0xfe, 0x10, 2, 7}, 1),
		"trap":                   bytes.Replace(b, []byte{0x6c}, []byte{0x6d}, 1),
		"escape":                 bytes.Replace(b, []byte{0x0d, 0}, []byte{0x0d, 1}, 1),
		"other-offset":           bytes.Replace(b, []byte{0x28, 2, 7}, []byte{0x28, 2, 8}, 1),
		"other-bias":             bytes.Replace(b, []byte{0x41, 0x80, 0x7f, 0x6a}, []byte{0x41, 1, 0x6a}, 1),
		"counter-base-alias":     bytes.ReplaceAll(b, []byte{0x20, 0}, []byte{0x20, 2}),
		"address-alias":          bytes.ReplaceAll(bytes.ReplaceAll(b, []byte{0x22, 5}, []byte{0x22, 4}), []byte{0x20, 5}, []byte{0x20, 4}),
		"accumulator-base-alias": bytes.ReplaceAll(bytes.ReplaceAll(b, []byte{0x21, 3}, []byte{0x21, 0}), []byte{0x20, 3}, []byte{0x20, 0}),
		"nonmultiple-bound":      pairedI32DotBody(-128, -128, 255),
		"zero-bound":             pairedI32DotBody(-128, -128, 0),
		"negative-bound":         pairedI32DotBody(-128, -128, -8),
	} {
		r := wasm.ReaderFrom(bad)
		if _, ok := InspectPairedI32DotLoop(&r, m); ok || r.Offset() != 0 {
			t.Fatalf("accepted %s", name)
		}
	}
	for _, bad := range []*wasm.Module{nil, {}, {Memories: []wasm.MemType{{Shared: true}}}, {Memories: []wasm.MemType{{Limits: wasm.Limits{Addr64: true}}}}} {
		r := wasm.ReaderFrom(b)
		if _, ok := InspectPairedI32DotLoop(&r, bad); ok {
			t.Fatalf("accepted memory %+v", bad)
		}
	}
}
