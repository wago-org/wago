//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"runtime"
	"testing"
	"unsafe"
)

func TestRV64ManagedFunctionAggregateTransactions(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		t.Run(map[gc.Profile]string{gc.ProfileThroughput: "throughput", gc.ProfileTiny: "tiny"}[profile], func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			c := rv64TableStageCompile(t, "function-aggregate-producer", rv64LocalMutableReferenceImportModule(false), false)
			defer c.Close()
			imports := NewImports()
			imports.Function("env", "target", slotHostFunc(func(HostModule, []uint64, []uint64) {}))
			producer := rv64NativeTokenInstantiate(t, rt, c, imports)
			defer producer.Close()
			secondProducer := rv64NativeTokenInstantiate(t, rt, c, imports)
			defer secondProducer.Close()
			secondDescriptor, ok := secondProducer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("no second producer descriptor")
			}

			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("no producer descriptor")
			}
			in, _ := ownedExceptionTestInstance(t, profile, nil)
			in.refStore = producer.refStore
			arrayDesc, err := gc.NewArrayDesc(1, gc.StorageFuncRefNull)
			if err != nil {
				t.Fatal(err)
			}
			structDesc, err := gc.NewStructDesc(2, []gc.StorageKind{gc.StorageFuncRefNull})
			if err != nil {
				t.Fatal(err)
			}
			if err = in.gc.AddTypes([]gc.TypeDesc{arrayDesc, structDesc}); err != nil {
				t.Fatal(err)
			}
			field := FieldTypeDescriptor{Mutable: true, Storage: StorageTypeDescriptor{Value: ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapFunc}}}}}
			in.c.Types = append(in.c.Types, DefinedTypeDescriptor{Kind: CompositeTypeArray, Array: field}, DefinedTypeDescriptor{Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field}})
			in.c.GCTypeDescs = []gc.TypeDesc{{}, arrayDesc, structDesc}
			in.gcTypeMap = &gcTypeMapping{localToDomain: []gc.TypeID{0, 1, 2}}
			in.c.requiredFeatures |= CoreFeatureExceptionHandling
			in.c.codeCache.setNativeGCABIVersion(gc.NativeABIVersion)
			public := in.publicGCState()
			var constructionSlots gc.Slots
			journal := newManagedFunctionConstruction(in.c, in.gc, &constructionSlots)
			if journal == nil {
				t.Fatal("function construction journal missing")
			}
			constructed, err := in.gc.NewArrayWithRoots(1, 2, gc.Value{Kind: gc.StorageFuncRefNull, Bits: descriptor}, journal)
			if err != nil {
				t.Fatal(err)
			}
			recordManagedFunctionConstruction(in.gc, in.c, constructed, 1)
			if err = in.gc.CollectFull(journal); err != nil {
				t.Fatal(err)
			}
			if _, err = in.gc.ObjectType(constructed); err != nil {
				t.Fatal("construction lost unpublished function object", err)
			}
			if err = journal.bind(in, nil); err != nil {
				t.Fatal(err)
			}
			if int(in.managedFunctionCollector(nil).ObjectEdgeCount(constructed)) != 1 {
				t.Fatal("construction published no producer edge")
			}
			journal.close()
			nextJournal := newManagedFunctionConstruction(in.c, in.gc, &constructionSlots)
			journal.close()
			if current, ok := managedFunctionConstructions.Load(in.gc); !ok || current != nextJournal {
				t.Fatal("old constructor removed a later journal")
			}
			nextJournal.close()
			var result [1]uint64
			call := func(helper uint32, args []uint64, roots gc.RootSet) {
				t.Helper()
				if !in.dispatchManagedFunctionAggregate(helper, args, result[:], roots, public) {
					t.Fatal("function operation was not intercepted")
				}
			}
			// Prime collector handle/storage capacity before both samples. Root
			// buffers and the graph/producer setup are outside the measurement.
			const live = 512
			numericID := gc.TypeID(in.gc.TypeCount())
			numericDesc, err := gc.NewArrayDesc(numericID, gc.StorageI64)
			if err != nil {
				t.Fatal(err)
			}
			if err = in.gc.AddTypes([]gc.TypeDesc{numericDesc}); err != nil {
				t.Fatal(err)
			}
			memoryRoots := make(managedFunctionEnvRoots, live)
			var memoryRootSet gc.RootSet = &memoryRoots
			for i := range memoryRoots {
				ref, err := in.gc.NewArrayWithRoots(numericID, 4, gc.Value{Kind: gc.StorageI64}, memoryRootSet)
				if err != nil {
					t.Fatal(err)
				}
				memoryRoots[i] = gc.Root(ref)
			}
			clear(memoryRoots)
			if err = in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			drainManagedFunctionReclaims(in.gc)
			measure := func(functions bool) int64 {
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				for i := range memoryRoots {
					if functions {
						call(gcArrayAllocUniform, []uint64{descriptor, 4, 1}, memoryRootSet)
						memoryRoots[i] = gc.Root(gc.Ref(result[0]))
					} else {
						ref, err := in.gc.NewArrayWithRoots(numericID, 4, gc.Value{Kind: gc.StorageI64}, memoryRootSet)
						if err != nil {
							t.Fatal(err)
						}
						memoryRoots[i] = gc.Root(ref)
					}
				}
				runtime.GC()
				runtime.ReadMemStats(&after)
				runtime.KeepAlive(memoryRootSet)
				bytes := int64(after.HeapAlloc) - int64(before.HeapAlloc)
				clear(memoryRoots)
				if err = in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
					t.Fatal(err)
				}
				drainManagedFunctionReclaims(in.gc)
				return bytes
			}
			baseline, functionBytes := measure(false), measure(true)
			arrayBytes, err := gc.ArraySize(arrayDesc, 4)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("live=%d primed numeric Go=%dB function Go=%dB additional=%.2f GoB/live; native array=%dB each +16B Env/producer; edge=%dB shadow header=%dB Env record=%dB graph=%dB collector sidecar=%dB", live, baseline, functionBytes, float64(functionBytes-baseline)/live, arrayBytes, unsafe.Sizeof(managedFunctionEdge{}), uintptr(0), unsafe.Sizeof(managedFunctionEnv{}), unsafe.Sizeof(managedFunctionGraph{}), unsafe.Sizeof(managedFunctionCollector{}))
			call(gcArrayAllocUniform, []uint64{descriptor, 4096, 1}, gc.EmptyRoots{})
			array := gc.Ref(uint32(result[0]))
			slot := in.gc.NewGlobalSlot(array)
			state := in.managedFunctionCollector(nil)
			check := func(count uint32) {
				t.Helper()
				edgeCount := state.ObjectEdgeCount(array)
				live := 0
				for index := uint32(0); index < edgeCount; index++ {
					edge := state.objectShadowEdge(array, index)
					if !edge.ref.IsNull() {
						live++
						if edge.count != count {
							t.Fatalf("edge count=%d want%d", edge.count, count)
						}
					}
				}
				if count == 0 && live != 0 || count != 0 && live != 1 {
					t.Fatalf("live edges=%d count=%d", live, count)
				}
			}
			check(4096)
			producer.Close()
			call(gcArrayFill, []uint64{uint64(array), 1, 0, 4095, 1}, managedFunctionEnvRoots{gc.Root(array)})
			check(1)
			call(gcArraySet, []uint64{uint64(array), 1, secondDescriptor, 1}, managedFunctionEnvRoots{gc.Root(array)})
			secondProducer.Close()
			if state.ObjectEdgeCount(array) != 2 {
				t.Fatal("second producer did not create one overflow edge")
			}
			if err = in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			drainManagedFunctionReclaims(in.gc)
			if !secondProducer.hasPhysicalResources() {
				t.Fatal("overflow edge lost second producer")
			}
			call(gcArraySet, []uint64{uint64(array), 1, 0, 1}, managedFunctionEnvRoots{gc.Root(array)})
			if err = in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			drainManagedFunctionReclaims(in.gc)
			if secondProducer.hasPhysicalResources() {
				t.Fatal("removed overflow edge retained second producer")
			}
			check(1)
			// Overlapping self-copy reads both ranges before it changes any raw words.
			call(gcArrayCopy, []uint64{uint64(array), 1, uint64(array), 0, 4095, 1, 1}, gc.EmptyRoots{})
			check(2)
			warmArgs := []uint64{uint64(array), 0, descriptor, 1}
			var warmRoots gc.RootSet = managedFunctionEnvRoots{gc.Root(array)}
			// Typed cold registries and reusable scratch keep the setter allocation
			// free, including strict pointer-check instrumentation.
			if allocations := testing.AllocsPerRun(1000, func() { call(gcArraySet, warmArgs, warmRoots) }); allocations != 0 {
				t.Fatalf("warm function setter allocations=%v", allocations)
			}
			if err = in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			drainManagedFunctionReclaims(in.gc)
			if !producer.hasPhysicalResources() {
				t.Fatal("live array lost closed producer")
			}
			old, _ := in.gc.ArrayGet(array, 0)
			panicked := false
			func() {
				defer func() { panicked = recover() != nil }()
				call(gcArrayFill, []uint64{uint64(array), 4096, 0, 1, 1}, gc.EmptyRoots{})
			}()
			after, _ := in.gc.ArrayGet(array, 0)
			if !panicked || old != after {
				t.Fatal("trapping fill changed bytes or failed to trap")
			}
			check(2)
			call(gcArrayFill, []uint64{uint64(array), 0, 0, 4096, 1}, managedFunctionEnvRoots{gc.Root(array)})
			check(0)
			if err = in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			drainManagedFunctionReclaims(in.gc)
			if producer.hasPhysicalResources() || len(state.envs) != 0 {
				t.Fatal("cleared function array retained producer")
			}
			if err = in.gc.SetGlobalSlot(slot, gc.Null()); err != nil {
				t.Fatal(err)
			}
			if err = in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			if len(state.shadows) != 0 || len(state.extraShadows) != 0 {
				t.Fatal("dead array retained shadow metadata")
			}
		})
	}
}
