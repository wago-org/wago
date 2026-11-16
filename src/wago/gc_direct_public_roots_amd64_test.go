//go:build linux && amd64 && (wago_gc_helper_only || wago_gc_direct_reads || wago_gc_direct_alloc || wago_gc_direct_refstore || wago_gc_direct_trusted || wago_gc_direct_trusted_store || wago_gc_direct_scalar_arrays || wago_gc_direct_trusted_arrays || (wago_gc_direct_trusted_loop || wago_gc_direct_weighted_sum || wago_gc_direct_weighted_sse2) || wago_gc_direct_array_alloc || wago_gc_direct_trusted_refstruct || wago_gc_direct_trusted_alloc || (wago_gc_direct_fill || wago_gc_direct_fill_sse2) || wago_gc_direct_copy)

package wago

import (
	"context"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func directPublicRootsModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 1, 0x7f, 1, 0x63, 0}, []byte{0x60, 0, 1, 0x7f}, []byte{0x60, 0, 0})),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("new", 0, 0), wasmtest.ExportEntry("read", 0, 1), wasmtest.ExportEntry("tick", 0, 2), wasmtest.ExportEntry("root", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 0, 0, 0x0b}), wasmtest.Code([]byte{0x23, 0, 0xfb, 2, 0, 0, 0x0b}), wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}))),
	)
}

func TestDirectGCPublicRootsSurviveRelocation(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), directPublicRootsModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	rt := NewRuntime()
	defer rt.Close()
	in, err := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true, CollectEveryAlloc: true, ForceMajorEveryMinor: true, NurseryBytes: 128, ThroughputHeapBytes: 64 << 10}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	values, err := in.InvokeValues(context.Background(), "new", ValueI32(91))
	if err != nil || len(values) != 1 {
		t.Fatal(values, err)
	}
	token := values[0].GCRef()
	defer in.ReleaseGCRef(token)
	entry := in.refStore.gcByToken[token.token]
	original := in.gc.GlobalSlot(entry.slot)
	if err = in.SetGlobalValue("root", values[0]); err != nil {
		t.Fatal(err)
	}
	moved := false
	for i := 0; i < 20; i++ {
		if _, err = in.Invoke("tick"); err != nil {
			t.Fatal(err)
		}
		moved = moved || in.gc.GlobalSlot(entry.slot) != original
		got, err := in.Invoke("read")
		if err != nil || len(got) != 1 || got[0] != 91 {
			t.Fatalf("native global after relocation: %v %v", got, err)
		}
		// Reuse the ORIGINAL public result token after relocation.
		if err = in.SetGlobalValue("root", values[0]); err != nil {
			t.Fatal(err)
		}
		fromGlobal, err := in.GlobalValue("root")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = in.Invoke("tick"); err != nil {
			t.Fatal(err)
		}
		if err = in.SetGlobalValue("root", fromGlobal); err != nil {
			t.Fatal(err)
		}
		got, err = in.Invoke("read")
		if err != nil || len(got) != 1 || got[0] != 91 {
			t.Fatalf("global token after relocation: %v %v", got, err)
		}
		in.ReleaseGCRef(fromGlobal.GCRef())
	}
	if !moved || in.gc.Stats().FullCollections == 0 {
		t.Fatal("fixture did not relocate a public root through major collection")
	}
}
