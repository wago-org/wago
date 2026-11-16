//go:build linux && amd64 && (wago_gc_compact_loop || wago_gc_compact_pair_loop || (wago_gc_compact_range_loop || wago_gc_compact_range_sse2) || wago_gc_compact_range_stride)

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
	"unsafe"
)

func compactLoopInstance(t *testing.T, data []byte) *Instance {
	t.Helper()
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	if c.gcTrustedDirectVersion != 0 || c.gcCompactNativeReceipt() == 0 {
		t.Fatal("missing receipt")
	}
	rt := NewRuntime()
	t.Cleanup(func() { rt.Close() })
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, CollectEveryAlloc: true, ForceMajorEveryMinor: true, NurseryBytes: 128, ThroughputHeapBytes: 64 << 10}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { in.Close() })
	return in
}

func TestCompactNativeGCLoopLease(t *testing.T) {
	for _, tc := range []struct {
		name         string
		storage, get byte
		value        int64
		want         uint64
	}{
		{"i32", 0x7f, 11, 7, 7}, {"i8s", 0x78, 12, -7, 0xfffffff9}, {"i8u", 0x78, 13, -7, 249},
		{"i16s", 0x77, 12, -7, 0xfffffff9}, {"i16u", 0x77, 13, -7, 65529}, {"i64", 0x7e, 11, 0x100000001, 0x100000001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := compactLoopInstance(t, gcLoopResolverTypedModule(nil, tc.storage, tc.get, tc.value, false))
			if got, e := in.Invoke("sum", 0); e != nil || got[0] != 0 {
				t.Fatalf("null skip %v %v", got, e)
			}
			if _, e := in.Invoke("sum", 1); e == nil {
				t.Fatal("missing null trap")
			}
			for _, length := range []uint64{0, 1, 2, 31, 127} {
				if _, e := in.Invoke("init", length); e != nil {
					t.Fatal(e)
				}
				got, e := in.Invoke("sum", length)
				want := tc.want * length
				if tc.storage != 0x7e {
					want = uint64(uint32(want))
				}
				if e != nil || got[0] != want {
					t.Fatalf("sum %v %v want%d", got, e, want)
				}
				if _, e := in.Invoke("sum", length+1); e == nil {
					t.Fatal("missing late bounds trap")
				}
			}
			if in.gc.Stats().FullCollections == 0 {
				t.Fatal("no moving collection")
			}
		})
	}
}

func TestCompactNativeGCLoopStoreAndTrapOrder(t *testing.T) {
	store := []byte{0x20, 1, 0x20, 2, 0x41, 3, 0xfb, 14, 0}
	in := compactLoopInstance(t, gcLoopResolverModule(store))
	if _, e := in.Invoke("init", 32); e != nil {
		t.Fatal(e)
	}
	if got, e := in.Invoke("sum", 32); e != nil || got[0] != 96 {
		t.Fatalf("store %v %v", got, e)
	}
	div := []byte{0x41, 1, 0x41, 0, 0x6e, 0x1a}
	trap := compactLoopInstance(t, gcLoopResolverModule(div))
	if _, e := trap.Invoke("sum", 0); e != nil {
		t.Fatal(e)
	}
	_, e := trap.Invoke("sum", 1)
	if e == nil {
		t.Fatal("missing divide trap")
	}
	first := e.Error()
	if _, e = trap.Invoke("init", 1); e != nil {
		t.Fatal(e)
	}
	if _, e = trap.Invoke("sum", 1); e == nil || e.Error() != first {
		t.Fatalf("trap reordered %v vs %s", e, first)
	}
}

// The receiver is assigned immediately before the loop with no intervening
// helper. Its current pinned register, not an old canonical slot, is authoritative.
func TestCompactNativeGCLoopFreshReceiver(t *testing.T) {
	body := []byte{3, 1, 0x63, 0, 1, 0x7f, 1, 0x7f,
		0x41, 1, 0x41, 32, 0xfb, 6, 0, 0x21, 1,
		0x41, 9, 0x41, 32, 0xfb, 6, 0, 0x21, 1,
		0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40,
		0x20, 3, 0x20, 1, 0x20, 2, 0xfb, 11, 0, 0x6a, 0x21, 3,
		0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x0b,
		// A collecting allocation after the lease, then read the relocated local.
		0x41, 0, 0x41, 1, 0xfb, 6, 0, 0x1a,
		0x20, 3, 0x20, 1, 0x41, 0, 0xfb, 11, 0, 0x6a, 0x0b}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	in := compactLoopInstance(t, data)
	for _, n := range []uint64{0, 1, 31, 32} {
		if got, e := in.Invoke("run", n); e != nil || got[0] != 9*n+9 {
			t.Fatalf("n=%d: %v %v", n, got, e)
		}
	}
}

func TestCompactNativeGCLoopFirstAccessValidation(t *testing.T) {
	data := gcLoopResolverTypedModule(nil, 0x7f, 11, 7, true)
	in := compactLoopInstance(t, data)
	_, e := in.Invoke("sum", 1)
	var trap *TrapError
	if !errors.As(e, &trap) {
		t.Fatal("missing null trap", e)
	}
	m, e := wasm.DecodeModule(data)
	if e != nil {
		t.Fatal(e)
	}
	pc := uint32(bytes.Index(m.Code[1].BodyBytes, []byte{0xfb, 11, 0})) + m.Code[1].LocalDeclBytes
	if len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != pc {
		t.Fatalf("nullPC %+v want%d", trap.Frames, pc)
	}
	if _, e = in.Invoke("init", 8); e != nil {
		t.Fatal(e)
	}
	if got, e := in.Invoke("sum", 7); e != nil || got[0] != 98 {
		t.Fatal(got, e)
	}
	if _, e = in.Invoke("sum", 8); e == nil {
		t.Fatal("missing second-access latebound")
	}
	v := in.gc.CompactView()
	arena := unsafe.Slice((*byte)(unsafe.Pointer(v.Arena)), int(v.ArenaBytes))
	ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
	typ := binary.LittleEndian.Uint32(arena[ref:])
	binary.LittleEndian.PutUint32(arena[ref:], 0xffffffff)
	defer binary.LittleEndian.PutUint32(arena[ref:], typ)
	if got, e := in.Invoke("sum", 0); e != nil || got[0] != 0 {
		t.Fatal("zero-trip inspected malformed receiver", got, e)
	}
	if _, e = in.Invoke("sum", 1); e == nil {
		t.Fatal("first access skipped fullvalidation")
	}
}
