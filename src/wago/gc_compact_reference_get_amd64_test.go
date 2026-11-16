//go:build linux && amd64 && (wago_gc_compact_reads || wago_gc_compact_alloc || wago_gc_compact_refstore || wago_gc_compact_scalar_arrays || wago_gc_compact_array_alloc || wago_gc_compact_fill || wago_gc_compact_copy || wago_gc_compact_refstruct)

package wago

import (
	"encoding/binary"
	runtime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func compactReferenceStructModule(nullable bool) []byte {
	ref := byte(0x63)
	if !nullable {
		ref = 0x64
	}
	init := []byte{0x41, 0xdb, 0, 0xfb, 0, 0, 0xfb, 0, 1, 0x24, 0, 0x23, 0, 0xfb, 2, 1, 0, 0xfb, 2, 0, 0, 0x0b}
	read := []byte{0x23, 0, 0xfb, 2, 1, 0, 0xfb, 2, 0, 0, 0x0b}
	hold := []byte{0x23, 0, 0xfb, 2, 1, 0, 0x10, 3, 0xfb, 2, 0, 0, 0x0b}
	holdOnly := []byte{0x23, 0, 0xfb, 2, 1, 0, 0xd0, 1, 0x24, 0, 0x10, 3, 0xfb, 2, 0, 0, 0x0b}
	id := []byte{0x20, 0, 0x20, 0, 0x20, 1, 0x1c, 1, 0x63, 0, 0x21, 0, 0x20, 1, 0x04, 0x63, 0, 0x20, 0, 0x05, 0x20, 0, 0x0b, 0x21, 0, 0x10, 3, 0x20, 0, 0x0f, 0x0b}
	flow := []byte{0x10, 0, 0x1a, 0x23, 0, 0xfb, 2, 1, 0, 0xd0, 1, 0x24, 0, 0x20, 0, 0x10, 6, 0xfb, 2, 0, 0, 0x0b}
	churn := []byte{}
	for i := 0; i < 40; i++ {
		churn = append(churn, 0xfb, 1, 0, 0x1a)
	}
	churn = append(churn, 0x0b)
	def := []byte{0xfb, 1, 1, 0xfb, 2, 1, 0, 0xd1, 0x0b}
	if !nullable {
		def = []byte{0x41, 1, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x5f, 1, ref, 0, 0}, []byte{0x60, 0, 1, 0x7f}, []byte{0x60, 0, 0}, []byte{0x60, 2, 0x63, 0, 0x7f, 1, 0x63, 0}, []byte{0x60, 1, 0x7f, 1, 0x7f})),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{2}, []byte{2}, []byte{3}, []byte{2}, []byte{2}, []byte{4}, []byte{5})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 1, 1, 0xd0, 1, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("read", 0, 1), wasmtest.ExportEntry("hold", 0, 2), wasmtest.ExportEntry("default", 0, 4), wasmtest.ExportEntry("hold_only", 0, 5), wasmtest.ExportEntry("flow", 0, 7))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(init), wasmtest.Code(read), wasmtest.Code(hold), wasmtest.Code(churn), wasmtest.Code(def), wasmtest.Code(holdOnly), wasmtest.Code(id), wasmtest.Code(flow))))
}

func TestCompactNativeReferenceStructRoots(t *testing.T) {
	for _, nullable := range []bool{true, false} {
		for _, forced := range []bool{false, true} {
			c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false), compactReferenceStructModule(nullable))
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			if c.gcCompactNativeReceipt() == 0 {
				t.Fatal("missing attestation")
			}
			rt := NewRuntime()
			defer rt.Close()
			in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, CollectEveryAlloc: forced, ForceMajorEveryMinor: true, NurseryBytes: 256, ThroughputHeapBytes: 64 << 10}})
			if e != nil {
				t.Fatal(e)
			}
			defer in.Close()
			got, e := in.Invoke("init")
			if e != nil || len(got) != 1 || got[0] != 91 {
				t.Fatal(got, e)
			}
			const sentinel = 0x81234567
			slot := in.ctrl[runtime.HostCtrlImportIndexOffset:]
			binary.LittleEndian.PutUint32(slot, sentinel)
			got, e = in.Invoke("read")
			if e != nil || len(got) != 1 || got[0] != 91 {
				t.Fatal(got, e)
			}
			if binary.LittleEndian.Uint32(slot) != sentinel {
				t.Fatal("reference getter entered helper")
			}
			if in.gc.Stats().Allocations != 2 {
				t.Fatal("constructor accounting")
			}
			got, e = in.Invoke("default")
			if e != nil || len(got) != 1 || got[0] != 1 {
				t.Fatal(got, e)
			}
			// Hold only the loaded child across an internal allocating call. Parent is
			// also retained by an owned global; both root paths must survive movement.
			for i := 0; i < 8; i++ {
				got, e = in.Invoke("hold")
				if e != nil || len(got) != 1 || got[0] != 91 {
					t.Fatal(nullable, forced, i, got, e)
				}
				got, e = in.Invoke("read")
				if e != nil || got[0] != 91 {
					t.Fatal(got, e)
				}
			}
			for i := 0; i < 8; i++ {
				if _, e := in.Invoke("init"); e != nil {
					t.Fatal(e)
				}
				got, e := in.Invoke("hold_only")
				if e != nil || len(got) != 1 || got[0] != 91 {
					t.Fatal("sole loaded child root", i, got, e)
				}
			}
			for i := 0; i < 8; i++ {
				got, e := in.Invoke("flow", uint64(i&1))
				if e != nil || len(got) != 1 || got[0] != 91 {
					t.Fatal("select/local/branch/call/return root", i, got, e)
				}
			}
			if in.gc.Stats().FullCollections == 0 {
				t.Fatal("no moving collections")
			}
		}
	}
}

func TestCompactNativeReferenceGetterRejectsMalformedReceiver(t *testing.T) {
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), compactReferenceStructModule(true))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	rt := NewRuntime()
	defer rt.Close()
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true}})
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	if _, e = in.Invoke("init"); e != nil {
		t.Fatal(e)
	}
	original := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
	defer writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, original)
	for _, bad := range []uint64{0, 3, original + 2, original + 16} {
		writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, bad)
		if _, e = in.Invoke("read"); e == nil {
			t.Fatalf("malformed receiver %d accepted", bad)
		}
	}
}
func TestCompactNativeI31ReferenceGetter(t *testing.T) {
	// The extracted i31 stays on the operand stack across an allocating call.
	// It must not be interpreted as a moving object root.
	churn := []byte{0xfb, 1, 0, 0x1a, 0x0b}
	run := []byte{0x41, 0x79, 0xfb, 0x1c, 0xfb, 0, 0, 0xfb, 2, 0, 0, 0x10, 1, 0xfb, 0x1d, 0x0b}
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x6c, 0}, []byte{0x60, 0, 1, 0x7f}, []byte{0x60, 0, 0})), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(run), wasmtest.Code(churn))))
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false), data)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	rt := NewRuntime()
	defer rt.Close()
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, CollectEveryAlloc: true, ForceMajorEveryMinor: true}})
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	for i := 0; i < 8; i++ {
		v, e := in.Invoke("run")
		if e != nil || len(v) != 1 || v[0] != uint64(uint32(0xfffffff9)) {
			t.Fatal(v, e)
		}
	}
}
