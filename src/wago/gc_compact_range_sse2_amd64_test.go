//go:build linux && amd64 && (wago_gc_compact_range_sse2 || wago_gc_compact_weighted_sum)

package wago

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
	"unsafe"
)

func compactSSETransferModule(storage, get byte, bound int32) []byte {
	initOp := byte(0x41)
	if storage == 0x7e {
		initOp = 0x42
	}
	init := []byte{0, initOp, 0, 0x41}
	init = append(init, wasmtest.SLEB32(512)...)
	init = append(init, 0xfb, 6, 0, 0x24, 0, 0x23, 0, 0x24, 1, 0x0b)
	body := []byte{1, 2, 0x63, 0, 0x23, 0, 0x21, 1, 0x23, 1, 0x21, 2, 0x03, 0x40,
		0x20, 2, 0x20, 0, 0x20, 1, 0x20, 0, 0xfb, get, 0, 0xfb, 14, 0,
		0x20, 0, 0x41, 1, 0x6a, 0x22, 0, 0x41}
	body = append(body, wasmtest.SLEB32(bound)...)
	body = append(body, 0x49, 0x0d, 0, 0x0b, 0x20, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, storage, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}, []byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("copy", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(init))), init...), append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestCompactNativeRangeSSE2Spans(t *testing.T) {
	for _, tc := range []struct {
		storage, get byte
		width        int
	}{{0x78, 12, 1}, {0x77, 12, 2}, {0x7f, 11, 4}, {0x7e, 11, 8}} {
		lanes := 16 / tc.width
		for _, bound := range []int{lanes, lanes + 1, lanes + 2, 2*lanes + 2, 32} {
			for _, delta := range []int{0, 16, -16, bound * tc.width, 256} {
				// A fabricated interior start permits deliberate overlapping numeric spans.
				// This private test fixture never crosses allocated backing or runs collection.
				// Adjacent cases need the same 16-byte alignment as real compact object starts.
				if delta%16 != 0 {
					continue
				}
				c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), compactSSETransferModule(tc.storage, tc.get, int32(bound)))
				if e != nil {
					t.Fatal(e)
				}
				rt := NewRuntime()
				in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, NurseryBytes: 64 << 10, ThroughputHeapBytes: 128 << 10}})
				if e != nil {
					t.Fatal(e)
				}
				if _, e = in.Invoke("init"); e != nil {
					t.Fatal(e)
				}
				view := in.gc.CompactView()
				arena := unsafe.Slice((*byte)(unsafe.Pointer(view.Arena)), int(view.ArenaBytes))
				starts := unsafe.Slice((*byte)(unsafe.Pointer(view.Starts)), int(view.StartCount))
				base := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
				src, dst := base, base
				if delta >= 0 {
					dst += uint32(delta)
				} else {
					src += uint32(-delta)
				}
				// Arbitrary bit patterns include NaN-shaped words without admitting float ops.
				for i := 0; i < 512*tc.width; i++ {
					arena[int(base)+8+i] = byte(i*37 + 0xc1)
				}
				for _, off := range []uint32{src, dst} {
					if off != base {
						binary.LittleEndian.PutUint32(arena[off:], binary.LittleEndian.Uint32(arena[base:]))
						binary.LittleEndian.PutUint32(arena[off+4:], 64)
						starts[off/8] = 1
					}
				}
				writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, uint64(src))
				writeGlobalObject(in.globalCells[1], in.c.Globals[1].Type, uint64(dst))
				original := append([]byte(nil), arena...)
				for _, first := range []int{0, bound - 1} {
					copy(arena, original)
					want := append([]byte(nil), arena...)
					for i := first; i < bound; i++ {
						copy(want[int(dst)+8+i*tc.width:int(dst)+8+(i+1)*tc.width], want[int(src)+8+i*tc.width:int(src)+8+(i+1)*tc.width])
					}
					got, err := in.Invoke("copy", uint64(first))
					if err != nil || len(got) != 1 || got[0] != uint64(bound) || !bytes.Equal(arena, want) {
						t.Fatalf("width%d bound%d delta%d first%d result%v error%v bytesEqual%v", tc.width, bound, delta, first, got, err, bytes.Equal(arena, want))
					}
				}
				for _, off := range []uint32{src, dst} {
					if off != base {
						starts[off/8] = 0
					}
				}
				writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, uint64(base))
				writeGlobalObject(in.globalCells[1], in.c.Globals[1].Type, uint64(base))
				in.Close()
				rt.Close()
				c.Close()
			}
		}
	}
}
