//go:build linux && amd64 && (wago_gc_compact_array_alloc || (wago_gc_compact_fill || (wago_gc_compact_copy || wago_gc_compact_loop)))

package wago

import (
	"encoding/binary"
	"fmt"
	runtime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
	"unsafe"
)

func compactArrayBumpModule(storage, value, get byte, zero bool) []byte {
	body := []byte{0x20, 0, 0x20, 1, 0xfb, 6, 0}
	if zero {
		body = []byte{0x20, 1, 0xfb, 7, 0}
	}
	body = append(body, 0x24, 0, 0x23, 0, 0x20, 2, 0xfb, get, 0, 0x0b)
	empty := []byte{0x41, 0, 0xfb, 7, 0, 0x24, 0, 0x23, 0, 0xfb, 15, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, storage, 1}, []byte{0x60, 3, value, 0x7f, 0x7f, 1, value}, []byte{0x60, 0, 1, 0x7f})),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("empty", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code(empty))))
}

func newCompactArrayBumpInstance(t *testing.T, data []byte, nursery uint32) *Instance {
	t.Helper()
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	rt := NewRuntime()
	t.Cleanup(func() { rt.Close() })
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, NurseryBytes: nursery, ThroughputHeapBytes: 1 << 20, ForceMajorEveryMinor: true}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { in.Close() })
	return in
}

func TestCompactNativeGCArrayBumpShapes(t *testing.T) {
	for _, tc := range []struct {
		storage, value, get byte
		arg, want           uint64
	}{
		{0x78, 0x7f, 12, 255, 0xffffffff}, {0x78, 0x7f, 13, 255, 255},
		{0x77, 0x7f, 12, 65535, 0xffffffff}, {0x77, 0x7f, 13, 65535, 65535},
		{0x7f, 0x7f, 11, 0xdeadbeef, 0xdeadbeef}, {0x7e, 0x7e, 11, 0x123456789abcdef, 0x123456789abcdef},
		{0x7d, 0x7d, 11, 0x7fc12345, 0x7fc12345}, {0x7c, 0x7c, 11, 0x8000000000000000, 0x8000000000000000},
	} {
		for _, zero := range []bool{false, true} {
			t.Run(fmt.Sprintf("%x/%d/%t", tc.storage, tc.get, zero), func(t *testing.T) {
				in := newCompactArrayBumpInstance(t, compactArrayBumpModule(tc.storage, tc.value, tc.get, zero), 256)
				slot := in.ctrl[runtime.HostCtrlImportIndexOffset:]
				const sentinel = 0x81234567
				for _, length := range []uint64{1, 3, 7} {
					v := in.gc.CompactView()
					arena := unsafe.Slice((*byte)(unsafe.Pointer(v.Arena)), int(v.ArenaBytes))
					for j := v.NurseryBump; j < v.NurseryEnd; j++ {
						arena[j] = 0xa5
					}
					binary.LittleEndian.PutUint32(slot, sentinel)
					before := in.gc.Stats().Allocations
					got, e := in.Invoke("run", tc.arg, length, length-1)
					want := tc.want
					if zero {
						want = 0
					}
					if e != nil || len(got) != 1 || got[0] != want {
						t.Fatal(length, got, e, want)
					}
					if binary.LittleEndian.Uint32(slot) != sentinel {
						t.Fatal("roomy array constructor entered helper")
					}
					if in.gc.Stats().Allocations != before+1 {
						t.Fatal("native accounting")
					}
					width := uint64(4)
					switch tc.storage {
					case 0x78:
						width = 1
					case 0x77:
						width = 2
					case 0x7e, 0x7c:
						width = 8
					}
					ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
					size := 8 + length*width
					stride := (size + 15) &^ uint64(15)
					if binary.LittleEndian.Uint32(arena[ref+4:]) != uint32(length) {
						t.Fatal("incorrect size or length")
					}
					for j := uint64(0); j < length*width; j++ {
						b := byte(tc.arg >> ((j % width) * 8))
						if zero {
							b = 0
						}
						if arena[uint64(ref)+8+j] != b {
							t.Fatal("uninitialized or incorrect payload", j)
						}
					}
					for j := 8 + length*width; j < stride; j++ {
						if arena[uint64(ref)+j] != 0 {
							t.Fatal("nonzero padding", j)
						}
					}

				}
				// Repeated publication and collection must preserve the owned global root.
				for i := 0; i < 32; i++ {
					got, e := in.Invoke("run", tc.arg, 7, 6)
					want := tc.want
					if zero {
						want = 0
					}
					if e != nil || len(got) != 1 || got[0] != want {
						t.Fatal(got, e)
					}
				}
				if in.gc.Stats().FullCollections == 0 {
					t.Fatal("no moving fallback")
				}
				got, e := in.Invoke("empty")
				if e != nil || len(got) != 1 || got[0] != 0 {
					t.Fatal(got, e)
				}
			})
		}
	}
}

func TestCompactNativeGCArrayBumpGuardedFallback(t *testing.T) {
	in := newCompactArrayBumpInstance(t, compactArrayBumpModule(0x7f, 0x7f, 11, false), 128<<10)
	slot := in.ctrl[runtime.HostCtrlImportIndexOffset:]
	const sentinel = 0x81234567
	for _, kind := range []string{"disabled", "bump-pointer", "starts", "start-count", "snapshot", "cap"} {
		// Private pointer/start-map corruption is checked-mode-only; policy5
		// relies on admitted ownership. Dynamic capability/capacity checks remain.
		v := in.gc.CompactView()
		length := uint64(3)
		switch kind {
		case "disabled":
			v.AllocationEnabled = 0
		case "bump-pointer":
			v.NurseryBumpAddress = 0
		case "starts":
			v.Starts = 0
		case "start-count":
			v.StartCount = 0
		case "snapshot":
			v.NurseryBump += 16
		case "cap":
			length = 17000
		}
		binary.LittleEndian.PutUint32(slot, sentinel)
		n := in.gc.Stats().Allocations
		got, e := in.Invoke("run", 91, length, length-1)
		if e != nil || len(got) != 1 || got[0] != 91 {
			t.Fatal(kind, got, e)
		}
		if binary.LittleEndian.Uint32(slot) == sentinel || in.gc.Stats().Allocations != n+1 {
			t.Fatal(kind, "missing fallback or double accounting")
		}
	}
	// Unsigned length multiplication must not wrap into a tiny native allocation.
	before := in.gc.Stats().Allocations
	if _, e := in.Invoke("run", 91, 0xffffffff, 0); e == nil {
		t.Fatal("overflow length accepted")
	}
	if in.gc.Stats().Allocations != before {
		t.Fatal("failed huge allocation published")
	}
}

func TestCompactNativeGCArrayBumpPreservesCallerRoots(t *testing.T) {
	for _, zero := range []bool{false, true} {
		alloc := []byte{0x41, 7, 0x20, 0, 0xfb, 6, 1, 0x0b}
		if zero {
			alloc = []byte{0x20, 0, 0xfb, 7, 1, 0x0b}
		}
		run := []byte{0x41, 0xdb, 0, 0xfb, 0, 0, 0x41, 40, 0x10, 0, 0x1a, 0xfb, 2, 0, 0, 0x0b}
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x5e, 0x7f, 1}, []byte{0x60, 1, 0x7f, 1, 0x64, 1}, []byte{0x60, 0, 1, 0x7f})),
			wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(alloc), wasmtest.Code(run))))
		c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false), data)
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		rt := NewRuntime()
		defer rt.Close()
		in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, NurseryBytes: 128, ThroughputHeapBytes: 64 << 10, ForceMajorEveryMinor: true}})
		if e != nil {
			t.Fatal(e)
		}
		defer in.Close()
		for i := 0; i < 32; i++ {
			got, e := in.Invoke("run")
			if e != nil || len(got) != 1 || got[0] != 91 {
				t.Fatal(zero, i, got, e)
			}
		}
		s := in.gc.Stats()
		if s.Allocations != 64 || s.FullCollections == 0 {
			t.Fatal("caller fallback accounting", s)
		}
	}
}

// Compact accounting counts completed nursery objects on retirement and includes the current
// nursery in Stats. Observation must neither charge twice nor require JIT stores.
func TestCompactNativeGCDeferredNativeAllocationAccounting(t *testing.T) {
	for _, array := range []bool{false, true} {
		data, args := compactBumpScalarModule(false), []uint64{91}
		if array {
			data, args = compactArrayBumpModule(0x7f, 0x7f, 11, false), []uint64{91, 3, 2}
		}
		in := newCompactArrayBumpInstance(t, data, 256)
		const sentinel = 0x81234567
		slot := in.ctrl[runtime.HostCtrlImportIndexOffset:]
		binary.LittleEndian.PutUint32(slot, sentinel)
		for i := uint64(1); i <= 33; i++ {
			got, e := in.Invoke("run", args...)
			if e != nil || len(got) != 1 || got[0] != 91 {
				t.Fatal(got, e)
			}
			if i == 1 && binary.LittleEndian.Uint32(slot) != sentinel {
				t.Fatal("native path unexpectedly needs accounting helper")
			}
			for n := 0; n < 3; n++ {
				if in.gc.Stats().Allocations != i {
					t.Fatal("observation changed accounting", i, in.gc.Stats())
				}
			}
		}
		before := in.gc.Stats()
		if before.FullCollections == 0 {
			t.Fatal("no nursery retirement")
		}
		collector := in.gc
		if e := in.Close(); e != nil {
			t.Fatal(e)
		}
		if collector.Stats().Allocations != before.Allocations {
			t.Fatal("close lost or duplicated pending objects")
		}
	}
}
