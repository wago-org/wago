//go:build linux && amd64 && (wago_gc_direct_alloc || wago_gc_direct_refstore || wago_gc_direct_trusted || wago_gc_direct_trusted_store || wago_gc_direct_scalar_arrays || wago_gc_direct_trusted_arrays || wago_gc_direct_trusted_loop || wago_gc_direct_array_alloc || wago_gc_direct_trusted_refstruct || wago_gc_direct_trusted_alloc || wago_gc_direct_fill || wago_gc_direct_copy)

package wago

import (
	"encoding/binary"
	runtime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func directBumpScalarModule(zero bool) []byte {
	body := []byte{0x20, 0, 0xfb, 0, 0, 0xfb, 2, 0, 0, 0x0b}
	if zero {
		body = []byte{0xfb, 1, 0, 0xfb, 2, 0, 0, 0x0b}
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 1, 0x7f, 1, 0x7f})), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestDirectGCBumpPublicationAndFallback(t *testing.T) {
	for _, zero := range []bool{false, true} {
		c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), directBumpScalarModule(zero))
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		rt := NewRuntime()
		defer rt.Close()
		in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true, NurseryBytes: 128, ThroughputHeapBytes: 64 << 10}})
		if e != nil {
			t.Fatal(e)
		}
		defer in.Close()
		const sentinel = 0x81234567
		index := in.ctrl[runtime.HostCtrlImportIndexOffset:]
		binary.LittleEndian.PutUint32(index, sentinel)
		before := in.gc.Stats()
		got, e := in.Invoke("run", 91)
		want := uint64(91)
		if zero {
			want = 0
		}
		if e != nil || len(got) != 1 || got[0] != want {
			t.Fatal(got, e)
		}
		if binary.LittleEndian.Uint32(index) != sentinel {
			t.Fatal("roomy constructor entered Go helper")
		}
		if s := in.gc.Stats(); s.Allocations != before.Allocations+1 || s.MinorCollections != before.MinorCollections {
			t.Fatalf("native accounting %+v -> %+v", before, s)
		}
		// The deliberately tiny nursery guarantees a slow-path collection. Subsequent
		// native allocations must see the helper's new authoritative bump and bounds.
		for i := 0; i < 32; i++ {
			got, e = in.Invoke("run", 91)
			if e != nil || len(got) != 1 || got[0] != want {
				t.Fatal(got, e)
			}
		}
		if binary.LittleEndian.Uint32(index) == sentinel {
			t.Fatal("full nursery never used rooted fallback")
		}
		if s := in.gc.Stats(); s.Allocations != before.Allocations+33 || s.MinorCollections == before.MinorCollections {
			t.Fatalf("mixed accounting %+v -> %+v", before, s)
		}
		// A stale read snapshot must decline before mutation; the helper repairs it.
		view := in.gc.DirectView()
		view.NurseryBump += 16
		binary.LittleEndian.PutUint32(index, sentinel)
		got, e = in.Invoke("run", 91)
		if e != nil || len(got) != 1 || got[0] != want {
			t.Fatal(got, e)
		}
		if binary.LittleEndian.Uint32(index) == sentinel {
			t.Fatal("stale snapshot bypassed helper")
		}
		if in.gc.Stats().Allocations != before.Allocations+34 {
			t.Fatal("stale fallback double-counted allocation")
		}

		for _, kind := range []string{"disabled", "bump-pointer", "end", "starts", "start-count"} {
			// Private pointer/start-map corruption is checked-mode-only; policy5
			// relies on admitted ownership. Dynamic capability/capacity checks remain.
			if in.c.gcTrustedDirectVersion == 5 && (kind == "bump-pointer" || kind == "starts" || kind == "start-count") {
				continue
			}
			v := in.gc.DirectView()
			switch kind {
			case "disabled":
				v.AllocationEnabled = 0
			case "bump-pointer":
				v.NurseryBumpAddress = 0
			case "end":
				v.NurseryEnd = v.NurseryBump - 1
			case "starts":
				v.Starts = 0
			case "start-count":
				v.StartCount = 0
			}
			n := in.gc.Stats().Allocations
			binary.LittleEndian.PutUint32(index, sentinel)
			got, e = in.Invoke("run", 91)
			if e != nil || len(got) != 1 || got[0] != want {
				t.Fatal(kind, got, e)
			}
			if binary.LittleEndian.Uint32(index) == sentinel || in.gc.Stats().Allocations != n+1 {
				t.Fatal(kind, "failed to decline safely")
			}
		}
	}
}

func TestDirectGCBumpKeepsCallerInitializerRoots(t *testing.T) {
	for _, kind := range []string{"struct", "uniform", "fixed"} {
		c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false), helperInitializerRewriteModule(kind))
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		rt := NewRuntime()
		defer rt.Close()
		in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true, NurseryBytes: 64, ThroughputHeapBytes: 64 << 10, ForceMajorEveryMinor: true}})
		if e != nil {
			t.Fatal(e)
		}
		defer in.Close()
		want, count := uint64(37), uint64(3)
		if kind == "fixed" {
			want, count = 74, 5
		}
		for i := 0; i < 16; i++ {
			got, e := in.Invoke("run")
			if e != nil || len(got) != 1 || got[0] != want {
				t.Fatal(kind, i, got, e)
			}
		}
		if s := in.gc.Stats(); s.Allocations != 16*count || s.FullCollections == 0 {
			t.Fatalf("%s accounting %+v", kind, s)
		}
	}
}

func TestDirectGCBumpBoundedShapes(t *testing.T) {
	for _, fields := range []int{0, 8, 9} {
		st := []byte{0x5f, byte(fields)}
		for i := 0; i < fields; i++ {
			st = append(st, 0x7e, 0)
		}
		data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(st, []byte{0x60, 0, 1, 0x7f})), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x41, 1, 0x0b}))))
		c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		rt := NewRuntime()
		defer rt.Close()
		in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true, NurseryBytes: 256, ThroughputHeapBytes: 64 << 10}})
		if e != nil {
			t.Fatal(e)
		}
		defer in.Close()
		const sentinel = 0x81234567
		index := in.ctrl[runtime.HostCtrlImportIndexOffset:]
		binary.LittleEndian.PutUint32(index, sentinel)
		got, e := in.Invoke("run")
		if e != nil || len(got) != 1 || got[0] != 1 {
			t.Fatal(got, e)
		}
		native := binary.LittleEndian.Uint32(index) == sentinel
		if native != (fields <= 8) {
			t.Fatalf("fields%d native%v", fields, native)
		}
		if in.gc.Stats().Allocations != 1 {
			t.Fatal("bad allocation accounting")
		}
	}
}
