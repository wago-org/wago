//go:build linux && amd64 && wago_gc_compact_refstruct

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	runtime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"os"
	"testing"
	"unsafe"
)

func compactRefConstructorModule(open ...bool) []byte {
	leaf := []byte{0x5f, 1, 0x7f, 0}
	if len(open) > 0 && open[0] {
		leaf = []byte{0x50, 0, 0x5f, 1, 0x7f, 0}
	}
	// Leaf0, parent1 with nullable final-leaf reference, different leaf2.
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(leaf, []byte{0x5f, 1, 0x63, 0, 0}, []byte{0x5f, 1, 0x7e, 0}, []byte{0x60, 1, 0x7f, 1, 0x63, 1}, []byte{0x60, 0, 1, 0x63, 1})),
		wasmtest.Section(3, wasmtest.Vec([]byte{3}, []byte{4}, []byte{4})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("make", 0, 0), wasmtest.ExportEntry("wrap", 0, 1), wasmtest.ExportEntry("default", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 0, 0, 0xfb, 0, 1, 0x0b}), wasmtest.Code([]byte{0x23, 0, 0xfb, 0, 1, 0x0b}), wasmtest.Code([]byte{0xfb, 1, 1, 0x0b}))))
}
func TestCompactNativeReferenceConstructors(t *testing.T) {
	for _, forced := range []bool{false, true} {
		c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), compactRefConstructorModule())
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		rt := NewRuntime()
		defer rt.Close()
		in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, CollectEveryAlloc: forced, ForceMajorEveryMinor: true, NurseryBytes: 64, ThroughputHeapBytes: 4096}})
		if e != nil {
			t.Fatal(e)
		}
		defer in.Close()
		const sentinel = 0x81234567
		slot := in.ctrl[runtime.HostCtrlImportIndexOffset:]
		binary.LittleEndian.PutUint32(slot, sentinel)
		for i := 0; i < 20; i++ {
			before := in.gc.Stats().Allocations
			v, e := in.InvokeValues(context.Background(), "make", ValueI32(int32(40+i)))
			if e != nil || len(v) != 1 {
				t.Fatal(v, e)
			}
			if i == 0 && !forced && binary.LittleEndian.Uint32(slot) != sentinel {
				t.Fatal("roomy reference constructor used helper")
			}
			if in.gc.Stats().Allocations != before+2 {
				t.Fatal("allocation accounting")
			}
			token := v[0].GCRef()
			entry := in.refStore.gcByToken[token.token]
			p, e := in.gc.CheckedGlobalSlot(entry.slot)
			if e != nil {
				t.Fatal(e)
			}
			child, e := in.gc.StructGet(p, 0)
			if e != nil {
				t.Fatal(e)
			}
			n, e := in.gc.StructGet(child.Ref, 0)
			if e != nil || n.Bits != uint64(40+i) {
				t.Fatal(n, e)
			}
			if e = in.ReleaseGCRef(token); e != nil {
				t.Fatal(e)
			}
		}
		if in.gc.Stats().FullCollections == 0 {
			t.Fatal("capacity fallback did not collect")
		}
		v, e := in.InvokeValues(context.Background(), "default")
		if e != nil {
			t.Fatal(e)
		}
		token := v[0].GCRef()
		entry := in.refStore.gcByToken[token.token]
		r := in.gc.GlobalSlot(entry.slot)
		field, e := in.gc.StructGet(r, 0)
		if e != nil || field.Ref != gc.Null() {
			t.Fatal(field, e)
		}
		in.ReleaseGCRef(token)
		// Forge only the internal source global. The checked native child resolver
		// must fall back before parent publication, and the helper rejects its type.
		wrong, e := in.gc.NewStructDefaultWithRoots(in.requireGCDomainType(2), gc.EmptyRoots{})
		if e != nil {
			t.Fatal(e)
		}
		original := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
		writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, uint64(wrong))
		before := in.gc.Stats().Allocations
		view := in.gc.CompactView()
		bump := view.NurseryBump
		arena := unsafe.Slice((*byte)(unsafe.Pointer(view.Arena)), int(view.ArenaBytes))
		image := append([]byte(nil), arena...)
		_, e = in.InvokeValues(context.Background(), "wrap")
		if view.NurseryBump != bump || !bytes.Equal(image, arena) {
			t.Fatal("rejected child mutated parent heap")
		}
		writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, original)
		if e == nil || in.gc.Stats().Allocations != before {
			t.Fatal("wrong child published parent", e)
		}
	}
}

func TestCompactReferenceConstructorRecursiveTree(t *testing.T) {
	data, e := os.ReadFile("../../bench/experiments/gc-v8/gc-tree-survival.wasm")
	if e != nil {
		t.Fatal(e)
	}
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	rt := NewRuntime()
	defer rt.Close()
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, NurseryBytes: 4096, ThroughputHeapBytes: 1 << 20}})
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	got, e := in.Invoke("performance", 32)
	if e != nil || len(got) != 1 || got[0] != 67551 {
		t.Fatal(got, e)
	}
	if in.gc.Stats().MinorCollections == 0 || in.gc.Stats().Allocations != 67551 {
		t.Fatal(in.gc.Stats())
	}
}
func TestCompactReferenceConstructorNonfinalFallback(t *testing.T) {
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), compactRefConstructorModule(true))
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
	const sentinel = 0x81234567
	slot := in.ctrl[runtime.HostCtrlImportIndexOffset:]
	binary.LittleEndian.PutUint32(slot, sentinel)
	v, e := in.InvokeValues(context.Background(), "make", ValueI32(31))
	if e != nil {
		t.Fatal(e)
	}
	defer in.ReleaseGCRef(v[0].GCRef())
	if binary.LittleEndian.Uint32(slot) == sentinel {
		t.Fatal("nonfinal child bypassed full helper")
	}
}

func TestCompactReferenceConstructorArrayAndNonnull(t *testing.T) {
	for _, vector := range []bool{false, true} {
		var leaf []byte
		var makeChild []byte
		if vector {
			leaf = []byte{0x5e, 0x7b, 1}
			makeChild = []byte{0x41, 2, 0xfb, 7, 0}
		} else {
			leaf = []byte{0x5f, 1, 0x7f, 0}
			makeChild = []byte{0x41, 7, 0xfb, 0, 0}
		}
		// The non-null constructor accepts only a live exact final child. The child
		// is temporarily held below the parent operation in canonical root slots.
		body := append(append([]byte{}, makeChild...), 0xfb, 0, 1, 0x0b)
		data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(leaf, []byte{0x5f, 1, 0x64, 0, 0}, []byte{0x60, 0, 1, 0x63, 1})), wasmtest.Section(3, wasmtest.Vec([]byte{2})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("make", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		for _, forced := range []bool{false, true} {
			c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if e != nil {
				t.Fatal(e)
			}
			rt := NewRuntime()
			in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, CollectEveryAlloc: forced, ForceMajorEveryMinor: true, NurseryBytes: 64, ThroughputHeapBytes: 4096}})
			if e != nil {
				t.Fatal(e)
			}
			for i := 0; i < 20; i++ {
				v, e := in.InvokeValues(context.Background(), "make")
				if e != nil {
					t.Fatal(vector, forced, e)
				}
				token := v[0].GCRef()
				entry := in.refStore.gcByToken[token.token]
				parent := in.gc.GlobalSlot(entry.slot)
				child, e := in.gc.StructGet(parent, 0)
				if e != nil || child.Ref.IsNull() {
					t.Fatal(child, e)
				}
				if vector {
					n, e := in.gc.ArrayLen(child.Ref)
					if e != nil || n != 2 {
						t.Fatal(n, e)
					}
					p, e := in.gc.ArrayPayload(child.Ref)
					if e != nil || len(p.Bytes) != 32 {
						t.Fatal(p, e)
					}
					for _, b := range p.Bytes {
						if b != 0 {
							t.Fatal("nonzero default SIMD payload")
						}
					}
				} else {
					n, e := in.gc.StructGet(child.Ref, 0)
					if e != nil || n.Bits != 7 {
						t.Fatal(n, e)
					}
				}
				in.ReleaseGCRef(token)
			}
			if in.gc.Stats().Allocations != 40 {
				t.Fatal(in.gc.Stats())
			}
			in.Close()
			rt.Close()
			c.Close()
		}
	}
}

func TestCompactReferenceConstructorAbstractFallback(t *testing.T) {
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x6c, 0}, []byte{0x60, 0, 1, 0x63, 0})), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("make", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0xfb, 0x1c, 0xfb, 0, 0, 0x0b}))))
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
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
	const sentinel = 0x81234567
	slot := in.ctrl[runtime.HostCtrlImportIndexOffset:]
	binary.LittleEndian.PutUint32(slot, sentinel)
	v, e := in.InvokeValues(context.Background(), "make")
	if e != nil {
		t.Fatal(e)
	}
	defer in.ReleaseGCRef(v[0].GCRef())
	if binary.LittleEndian.Uint32(slot) == sentinel {
		t.Fatal("abstract i31 constructor bypassed helper")
	}
	entry := in.refStore.gcByToken[v[0].GCRef().token]
	field, e := in.gc.StructGet(in.gc.GlobalSlot(entry.slot), 0)
	if e != nil || field.Ref != gc.I31New(7) {
		t.Fatal(field, e)
	}
	bad := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x5f, 1, 0x64, 0, 0}, []byte{0x60, 0, 1, 0x63, 1})), wasmtest.Section(3, wasmtest.Vec([]byte{2})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("make", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 1, 0x0b}))))
	if rejected, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), bad); e == nil {
		rejected.Close()
		t.Fatal("nonnullable default constructor accepted")
	}
}
