//go:build amd64 && (wago_gc_compact_pair_loop || wago_gc_compact_range_loop || wago_gc_compact_range_stride)

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestCompactPairLoopCertificate(t *testing.T) {
	read := func(local byte) []byte { return []byte{0x20, local, 0x41, 0, 0xfb, 11, 0, 0x1a} }
	end := []byte{0x41, 0, 0x0d, 0, 0x0b}
	for _, tc := range []struct {
		name   string
		locals []byte
		want   bool
	}{
		{"two", []byte{0, 2}, true}, {"same", []byte{0, 0}, false}, {"one", []byte{0}, false}, {"three", []byte{0, 2, 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixedArrayRootEmitter(wasm.I32, 0)
			f.m.Types[0].SubTypes[0].Final = true
			f.localType = []machineType{mtI64, mtI32, mtI64}
			var body []byte
			for _, l := range tc.locals {
				body = append(body, read(l)...)
			}
			body = append(body, end...)
			r := wasm.ReaderFrom(body)
			p, ok := f.planGCLoopResolverModePair(&r, true, true)
			if ok != tc.want {
				t.Fatalf("admission=%v", ok)
			}
			if ok && (p.local != 0 || p.secondLocal != 2 || p.accesses != 2) {
				t.Fatalf("certificate %+v", p)
			}
			if r.Offset() != 0 {
				t.Fatal("advanced reader")
			}
		})
	}
}

func TestCompactPairLoopPressure(t *testing.T) {
	body := []byte{0x20, 2, 0x41, 0, 0x20, 0, 0x41, 0, 0xfb, 11, 0, 0xfb, 14, 0, 0x41, 0, 0x0d, 0, 0x0b}
	for _, free := range []int{6, 7, 8} {
		f := fixedArrayRootEmitter(wasm.I32, 0)
		f.m.Types[0].SubTypes[0].Final = true
		f.localType = []machineType{mtI64, mtI32, mtI64}
		for i, r := range gpAlloc {
			if i >= free {
				f.reserved = f.reserved.add(r)
			}
		}
		r := wasm.ReaderFrom(body)
		f.prepareCompactLoop(&r)
		if got := f.directLoop.kind == loopLeaseCompactPair; got != (free >= 7) {
			t.Fatalf("free%d pair=%v", free, got)
		}
		if free >= 7 && (f.directLoop.local != 2<<16 || f.directLoop.remaining != 2) {
			t.Fatalf("invalid lease %+v", f.directLoop)
		}
	}
}
