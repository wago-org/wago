//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
)

type compactExternLooseTestRoots struct {
	owner *Instance
	word  uint64
}

func (r compactExternLooseTestRoots) RangeRoots(fn func(gc.RootSlot) bool) {
	visitCompactExternFrameRoot(r.owner, r.owner.gc, r.word, fn, nil)
}
func (r compactExternLooseTestRoots) RangeRootRefs(sink gc.RootRefSink) bool {
	return visitCompactExternFrameRoot(r.owner, r.owner.gc, r.word, nil, sink)
}

func TestRV64CompactExternAggregateEdges(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		t.Run(fmt.Sprint(profile), func(t *testing.T) {
			in, _ := ownedExceptionTestInstance(t, profile, nil)
			leafDesc, _ := gc.NewStructDesc(0, []gc.StorageKind{gc.StorageI32})
			arrayDesc, _ := gc.NewArrayDesc(1, gc.StorageExternRefNull)
			structDesc, _ := gc.NewStructDesc(2, []gc.StorageKind{gc.StorageExternRefNull})
			if err := in.gc.AddTypes([]gc.TypeDesc{arrayDesc, structDesc}); err != nil {
				t.Fatal(err)
			}
			field := FieldTypeDescriptor{Mutable: true, Storage: StorageTypeDescriptor{Value: ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}}}}
			in.c.Types = append(in.c.Types, DefinedTypeDescriptor{Kind: CompositeTypeArray, Array: field}, DefinedTypeDescriptor{Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field}})
			in.c.GCTypeDescs = []gc.TypeDesc{leafDesc, arrayDesc, structDesc}
			in.gcTypeMap = &gcTypeMapping{localToDomain: []gc.TypeID{0, 1, 2}}
			in.c.requiredFeatures |= CoreFeatureExceptionHandling
			in.c.codeCache.setNativeGCABIVersion(gc.NativeABIVersion)
			in.c.validateMemo.gcFrameRoots = &compiledGCFrameRoots{}
			public := in.publicGCState()
			var result [1]uint64
			call := func(helper uint32, args []uint64, roots gc.RootSet) {
				t.Helper()
				if !in.dispatchManagedFunctionAggregate(helper, args, result[:], roots, public) {
					t.Fatal("opaque aggregate helper was not intercepted", helper)
				}
			}
			collect := func(roots gc.RootSet) {
				t.Helper()
				if err := in.gc.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				drainExceptionReclaims(in.gc)
			}
			state, err := in.compactExternCollector(nil)
			if err != nil {
				t.Fatal(err)
			}
			leaf, err := in.gc.NewStructWithRoots(0, []gc.Value{gc.I32Value(42)}, gc.EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			word, err := state.externFromAny(leaf, gc.RefSliceRoots{leaf})
			if err != nil {
				t.Fatal(err)
			}
			_, shell := compactExternWordRoot(in.refStore, word)
			call(gcStructAllocOne, []uint64{word, 2}, gc.RefSliceRoots{shell})
			box := gc.Ref(uint32(result[0]))
			var boxRoots gc.RootSet = gc.RefSliceRoots{box}
			collect(boxRoots)
			inner, err := state.anyFromExtern(word, boxRoots)
			if err != nil || inner != leaf {
				t.Fatal("box lost converted object", inner, err)
			}
			got, err := in.gc.StructGet(inner, 0)
			if err != nil || got.Bits != 42 {
				t.Fatal("box payload", got, err)
			}
			foreign, err := in.refStore.issueExternref("aggregate foreign value")
			if err != nil {
				t.Fatal(err)
			}
			setArgs := []uint64{uint64(box), foreign, 2, 0}
			call(gcStructSet, setArgs, boxRoots)
			if in.refStore.releaseExternref(foreign) {
				t.Fatal("box lost foreign anchor pin")
			}
			collect(boxRoots)
			if len(state.shells) != 0 {
				t.Fatal("replaced box retained old shell")
			}
			// The object can die while only the raw extern value remains in a native
			// local. The root visitor must retain its foreign anchor.
			var loose gc.RootSet = compactExternLooseTestRoots{in, foreign}
			collect(loose)
			if _, err := in.gc.ObjectType(box); err == nil {
				t.Fatal("loose extern root retained its dead box")
			}
			if in.refStore.releaseExternref(foreign) {
				t.Fatal("loose extern local lost its foreign anchor")
			}
			collect(gc.EmptyRoots{})
			if !in.refStore.releaseExternref(foreign) {
				t.Fatal("dead loose local retained its foreign anchor")
			}

			foreign, err = in.refStore.issueExternref("array foreign value")
			if err != nil {
				t.Fatal(err)
			}
			call(gcArrayAllocUniform, []uint64{foreign, 4, 1}, gc.EmptyRoots{})
			source := gc.Ref(uint32(result[0]))
			var sourceRoots gc.RootSet = gc.RefSliceRoots{source}
			destination, err := in.gc.NewArrayDefaultWithRoots(1, 4, sourceRoots)
			if err != nil {
				t.Fatal(err)
			}
			var both gc.RootSet = gc.RefSliceRoots{source, destination}
			call(gcArrayCopy, []uint64{uint64(destination), 0, uint64(source), 0, 4, 1, 1}, both)
			call(gcArrayFill, []uint64{uint64(source), 0, 0, 4, 1}, both)
			var destinationRoots gc.RootSet = gc.RefSliceRoots{destination}
			collect(destinationRoots)
			if in.refStore.releaseExternref(foreign) {
				t.Fatal("copied array lost its foreign anchor")
			}
			if got, err := in.gc.ArrayGet(destination, 3); err != nil || got.Bits != foreign {
				t.Fatal("copied extern value", got, err)
			}
			setArgs = []uint64{uint64(destination), 0, foreign, 1}
			call(gcArraySet, setArgs, destinationRoots)
			if allocations := testing.AllocsPerRun(1000, func() {
				if !in.dispatchManagedFunctionAggregate(gcArraySet, setArgs, result[:], destinationRoots, public) {
					panic("lost extern setter")
				}
			}); allocations != 0 {
				t.Fatal("warm extern setter allocations", allocations)
			}
			call(gcArrayFill, []uint64{uint64(destination), 0, 0, 4, 1}, destinationRoots)
			collect(destinationRoots)
			if !in.refStore.releaseExternref(foreign) {
				t.Fatal("cleared arrays retained foreign anchor")
			}
			collect(gc.EmptyRoots{})
			if len(state.foreign) != 0 || len(state.shells) != 0 {
				t.Fatal("dead aggregate retained extern metadata")
			}
		})
	}
}
