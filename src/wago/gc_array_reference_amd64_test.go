//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

const stagedGCArrayReferenceHex = "0061736d0100000001cd808080000f5e78005e6400005e6400015e6300005e6e0160000164016000016403600001640460037f7f6401017f60027f7f017f60047f7f64027f017f60037f7f7f017f6001646a017f6000017f600000038c808080000b0505060708090a0b0c0d0e07b88080800006036e657700000c6e65772d6f766572666c6f770001036765740005077365745f6765740007036c656e00090964726f705f73656773000a099680808000010564000241074103fb06000b41014102fb0800020b0abf818080000b8a808080000041004102fb0a01000b928080800000418080808078418080808078fb0a01000b8a808080000041004102fb0a03000b8a808080000041004102fb0a04000b8e808080000020022000fb0b012001fb0d000b8a808080000020002001100010040b9c80808000002002200020022003fb0b02fb0e0220022000fb0b022001fb0d000b9280808000002000200141004102fb0a0200200210060b8680808000002000fb0f0b868080800000100010080b858080800000fc0d000b"

func stagedGCArrayReferenceBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCArrayReferenceHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStagedGCArrayReferenceElementSegmentRoots(t *testing.T) {
	data := stagedGCArrayReferenceBytes(t)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithFeatures(m, wasm.ValidationFeatures{GCConstExpr: true}); err != nil {
		t.Fatal(err)
	}
	init, err := stagedGCArrayElementInitializer(m)
	if err != nil {
		t.Fatal(err)
	}
	if init.SegmentIndex != 0 || init.TypeID != 0 || init.Count != 2 || init.Values[0].Mode != gcArrayGlobalInitUniform || init.Values[0].Length != 3 || init.Values[0].Bits[0] != 7 || init.Values[1].Mode != gcArrayGlobalInitFixed || init.Values[1].Length != 2 || init.Values[1].Bits[0] != 1 || init.Values[1].Bits[1] != 2 {
		t.Fatalf("reference element initializer = %+v", init)
	}
	descs, err := frontend.BuildGCTypeDescs(m)
	if err != nil {
		t.Fatal(err)
	}
	profiles := []struct {
		name string
		cfg  gc.Config
	}{
		{name: "throughput", cfg: gc.Config{CollectEveryAlloc: true, StressNurseryBytes: 48, VerifyAfterCollect: true}},
		{name: "tiny", cfg: gc.Config{Profile: gc.ProfileTiny, TinyHeapBytes: 48, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			collector, err := gc.NewCollector(tc.cfg, descs)
			if err != nil {
				t.Fatal(err)
			}
			defer collector.Close()
			descriptor := make([]byte, 16)
			state, err := instantiateGCArrayElementSegment(collector, descs, init, descriptor)
			if err != nil {
				t.Fatal(err)
			}
			if binary.LittleEndian.Uint64(descriptor) != 0 || binary.LittleEndian.Uint32(descriptor[8:]) != 2 || state.Count != 2 {
				t.Fatalf("segment descriptor/state = %x/%+v", descriptor, state)
			}
			for i := uint8(0); i < state.Count; i++ {
				rooted, err := collector.CheckedTableSlot(state.Slots[i])
				if err != nil || rooted != state.Refs[i] || !rooted.IsObj() {
					t.Fatalf("segment root %d = %v, %v; want %v", i, rooted, err, state.Refs[i])
				}
			}
			if got, err := collector.ArrayGet(state.Refs[0], 0); err != nil || got.Bits != 7 {
				t.Fatalf("uniform element = %+v, %v", got, err)
			}
			if got, err := collector.ArrayGet(state.Refs[1], 1); err != nil || got.Bits != 2 {
				t.Fatalf("fixed element = %+v, %v", got, err)
			}
			if err := collector.CollectFull(nil); err != nil {
				t.Fatal(err)
			}
			if collector.Stats().LiveObjects != 2 {
				t.Fatalf("rooted segment live objects = %d, want 2", collector.Stats().LiveObjects)
			}
			state.drop(collector)
			if binary.LittleEndian.Uint32(descriptor[8:]) != 0 {
				t.Fatalf("dropped descriptor = %x", descriptor)
			}
			for i := uint8(0); i < state.Count; i++ {
				if rooted, err := collector.CheckedTableSlot(state.Slots[i]); err != nil || !rooted.IsNull() {
					t.Fatalf("dropped segment root %d = %v, %v", i, rooted, err)
				}
			}
			if err := collector.CollectFull(nil); err != nil {
				t.Fatal(err)
			}
			if collector.Stats().LiveObjects != 0 {
				t.Fatalf("dropped segment live objects = %d, want 0", collector.Stats().LiveObjects)
			}
		})
	}
}

func TestStagedGCArrayReferenceElementAllocationAndDrop(t *testing.T) {
	data := stagedGCArrayReferenceBytes(t)
	c, err := compileStagedGCArray(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.stagedGCArrayProduct() != stagedGCArrayProductReferenceElements || !c.usesGCArrayHelpers() || c.memoryDir == nil || c.memoryDir.gcArrayElement == nil || len(c.passiveElems) != 1 {
		t.Fatalf("reference product/helper/segment = %v/%v/%+v/%d", c.stagedGCArrayProduct(), c.usesGCArrayHelpers(), c.memoryDir, len(c.passiveElems))
	}
	profiles := []struct {
		name string
		cfg  GCConfig
	}{
		{name: "throughput", cfg: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 80, VerifyAfterCollect: true}},
		{name: "tiny", cfg: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 80, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			in, err := instantiateCore(c, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			state := in.existingGCArrayElementState()
			if state == nil || state.Count != 2 || binary.LittleEndian.Uint32(state.Descriptor[8:]) != 2 {
				t.Fatalf("instance element state = %+v", state)
			}

			var result [1]uint64
			in.dispatchGCArrayHelper(gcArrayAllocElem, []uint64{0, 2, 1, 0}, result[:])
			outer := gc.Ref(uint32(result[0]))
			first, err := in.gc.ArrayGet(outer, 0)
			if err != nil {
				t.Fatal(err)
			}
			second, err := in.gc.ArrayGet(outer, 1)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := in.gc.ArrayGet(first.Ref, 0); err != nil || got.Bits != 7 {
				t.Fatalf("outer[0][0] = %+v, %v", got, err)
			}
			if got, err := in.gc.ArrayGet(second.Ref, 0); err != nil || got.Bits != 1 {
				t.Fatalf("outer[1][0] = %+v, %v", got, err)
			}

			for _, widenedType := range []uint64{3, 4} {
				in.dispatchGCArrayHelper(gcArrayAllocElem, []uint64{0, 2, widenedType, 0}, result[:])
				widened := gc.Ref(uint32(result[0]))
				got, err := in.gc.ArrayGet(widened, 1)
				if err != nil || got.Ref != second.Ref {
					t.Fatalf("widened array type %d element = %+v, %v; want %v", widenedType, got, err, second.Ref)
				}
			}

			in.dispatchGCArrayHelper(gcArrayAllocElem, []uint64{0, 2, 2, 0}, result[:])
			mutable := gc.Ref(uint32(result[0]))
			in.dispatchGCArrayHelper(gcArraySet, []uint64{uint64(mutable), 0, uint64(second.Ref), 2}, nil)
			gotRef, err := in.gc.ArrayGet(mutable, 0)
			if err != nil || gotRef.Ref != second.Ref {
				t.Fatalf("reference array.set = %+v, %v; want %v", gotRef, err, second.Ref)
			}

			before := in.gc.Stats()
			if _, err := in.Invoke("new-overflow"); err == nil {
				t.Fatal("overflowing array.new_elem succeeded")
			}
			after := in.gc.Stats()
			if after.Allocations != before.Allocations || after.LiveObjects != before.LiveObjects {
				t.Fatalf("overflowing array.new_elem changed collector state: before=%+v after=%+v", before, after)
			}
			if _, err := in.Invoke("drop_segs"); err != nil {
				t.Fatal(err)
			}
			if binary.LittleEndian.Uint32(state.Descriptor[8:]) != 0 {
				t.Fatalf("dropped instance descriptor = %x", state.Descriptor)
			}
			for i := uint8(0); i < state.Count; i++ {
				if rooted, err := in.gc.CheckedTableSlot(state.Slots[i]); err != nil || !rooted.IsNull() {
					t.Fatalf("dropped instance root %d = %v, %v", i, rooted, err)
				}
			}
			before = in.gc.Stats()
			if _, err := in.Invoke("new"); err == nil {
				t.Fatal("array.new_elem after elem.drop succeeded")
			}
			after = in.gc.Stats()
			if after.Allocations != before.Allocations || after.LiveObjects != before.LiveObjects {
				t.Fatalf("post-drop array.new_elem changed collector state: before=%+v after=%+v", before, after)
			}
			in.dispatchGCArrayHelper(gcArrayAllocElem, []uint64{0, 0, 1, 0}, result[:])
			if length, err := in.gc.ArrayLen(gc.Ref(uint32(result[0]))); err != nil || length != 0 {
				t.Fatalf("zero-length array.new_elem after drop = %d, %v", length, err)
			}
		})
	}
}

func TestStagedGCArrayReferenceOfficialProduct(t *testing.T) {
	data := stagedGCArrayReferenceBytes(t)
	if _, err := Compile(NewRuntimeConfig(), data); err == nil {
		t.Fatal("public compile unexpectedly admitted reference GC arrays")
	}
	c, err := compileStagedGCArray(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.stagedGCArrayProduct() != stagedGCArrayProductReferenceElements || c.memoryDir == nil || c.memoryDir.gcArrayElement == nil {
		t.Fatalf("reference product metadata = %v/%+v", c.stagedGCArrayProduct(), c.memoryDir)
	}
	if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "GC") {
		t.Fatalf("reference array snapshot = %v, want fail-closed GC rejection", err)
	}
	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.usesGCArrayHelpers() || loaded.stagedGCArrayProduct() != 0 || (loaded.memoryDir != nil && loaded.memoryDir.gcArrayElement != nil) {
		t.Fatal("codec reload inherited reference array helper/product/segment admission")
	}

	profiles := []struct {
		name string
		cfg  GCConfig
	}{
		{name: "throughput", cfg: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 96, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}},
		{name: "tiny", cfg: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 96, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, TinyStepBudget: 1, VerifyAfterCollect: true, StressBarriers: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			in, err := instantiateCore(c, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()

			raw, err := in.Invoke("new")
			if err != nil || len(raw) != 1 || raw[0] == 0 || raw[0]>>32 == 0 {
				t.Fatalf("new = %v, %v; want opaque GC token", raw, err)
			}
			token := raw[0]
			exact, owner, _, ok := in.refStore.gcRefExactType(token)
			if !ok || owner != in || exact.Kind != ValueTypeReference || !exact.Ref.Exact || !exact.Ref.Heap.Defined || exact.Ref.Heap.TypeIndex != 1 {
				t.Fatalf("reference exact token = %#v owner=%p ok=%v", exact, owner, ok)
			}
			if _, err := in.Invoke("new"); err == nil || !strings.Contains(err.Error(), "one live token") {
				t.Fatalf("second live reference token = %v", err)
			}
			if err := in.ReleaseGCRef(ValueOf(ValAnyRef, token).GCRef()); err != nil {
				t.Fatal(err)
			}
			values, err := in.Call(context.Background(), "new")
			if err != nil || len(values) != 1 || values[0].GCRef().IsNull() {
				t.Fatalf("Call new = %v, %v", values, err)
			}
			if err := in.ReleaseGCRef(values[0].GCRef()); err != nil {
				t.Fatal(err)
			}

			for _, action := range []struct {
				name string
				args []uint64
				want uint64
			}{
				{name: "get", args: []uint64{0, 0}, want: 7},
				{name: "get", args: []uint64{1, 0}, want: 1},
				{name: "set_get", args: []uint64{0, 1, 1}, want: 2},
				{name: "len", want: 2},
			} {
				got, err := in.Invoke(action.name, action.args...)
				if err != nil || len(got) != 1 || got[0] != action.want {
					t.Fatalf("%s%v = %v, %v; want %d", action.name, action.args, got, err, action.want)
				}
			}
			before := in.gc.Stats()
			if _, err := in.Invoke("new-overflow"); err == nil {
				t.Fatal("new-overflow succeeded")
			}
			after := in.gc.Stats()
			if after.Allocations != before.Allocations || after.LiveObjects != before.LiveObjects {
				t.Fatalf("overflowing array.new_elem changed collector state: before=%+v after=%+v", before, after)
			}
			for _, trap := range []struct {
				name string
				args []uint64
			}{
				{name: "get", args: []uint64{10, 0}},
				{name: "set_get", args: []uint64{10, 0, 0}},
			} {
				if _, err := in.Invoke(trap.name, trap.args...); err == nil {
					t.Fatalf("%s%v succeeded", trap.name, trap.args)
				}
			}
			if _, err := in.Invoke("drop_segs"); err != nil {
				t.Fatal(err)
			}
			if _, err := in.Invoke("new"); err == nil {
				t.Fatal("new after elem.drop succeeded")
			}
			if _, err := in.Invoke("new-overflow"); err == nil {
				t.Fatal("new-overflow after elem.drop succeeded")
			}
		})
	}

	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := in.Invoke("new")
	if err != nil || len(raw) != 1 {
		t.Fatalf("close-order new = %v, %v", raw, err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := in.ReleaseGCRef(ValueOf(ValAnyRef, raw[0]).GCRef()); err != nil {
		t.Fatalf("release token after producer close: %v", err)
	}
}

func TestStagedGCArrayReferenceFootprint(t *testing.T) {
	for name, got := range map[string]uintptr{
		"gcArrayElementInit":      unsafe.Sizeof(gcArrayElementInit{}),
		"gcArrayElementState":     unsafe.Sizeof(gcArrayElementState{}),
		"compiledMemoryDirectory": unsafe.Sizeof(compiledMemoryDirectory{}),
		"instancePluginState":     unsafe.Sizeof(instancePluginState{}),
	} {
		want := map[string]uintptr{"gcArrayElementInit": 96, "gcArrayElementState": 56, "compiledMemoryDirectory": 128, "instancePluginState": 136}[name]
		if got != want {
			t.Fatalf("%s size = %d, want %d", name, got, want)
		}
	}
}

func TestStagedGCArrayReferenceElementSegmentTinyRollback(t *testing.T) {
	m, err := wasm.DecodeModule(stagedGCArrayReferenceBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	init, err := stagedGCArrayElementInitializer(m)
	if err != nil {
		t.Fatal(err)
	}
	descs, err := frontend.BuildGCTypeDescs(m)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := gc.NewCollector(gc.Config{Profile: gc.ProfileTiny, TinyHeapBytes: 24, TinyBlockBytes: 8}, descs)
	if err != nil {
		t.Fatal(err)
	}
	defer collector.Close()
	descriptor := make([]byte, 16)
	if _, err := instantiateGCArrayElementSegment(collector, descs, init, descriptor); err == nil || !strings.Contains(err.Error(), "tiny heap exhausted") {
		t.Fatalf("Tiny element rollback = %v", err)
	}
	if binary.LittleEndian.Uint32(descriptor[8:]) != 0 {
		t.Fatalf("failed descriptor = %x", descriptor)
	}
	if err := collector.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if collector.Stats().LiveObjects != 0 {
		t.Fatalf("failed segment retained %d live objects", collector.Stats().LiveObjects)
	}
}

func BenchmarkStagedGCArrayReferenceGet(b *testing.B) {
	c, err := compileStagedGCArray(stagedGCArrayReferenceBytes(b))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := in.Invoke("get", 0, 0); err != nil || len(got) != 1 || got[0] != 7 {
			b.Fatalf("get = %v, %v", got, err)
		}
	}
}
