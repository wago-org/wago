package gc

import (
	"testing"
	"unsafe"
)

func TestNativeCollectorViewLayoutAndRefresh(t *testing.T) {
	if got := unsafe.Sizeof(handleEntry{}); got != NativeHandleStride {
		t.Fatalf("handle entry size = %d, want %d", got, NativeHandleStride)
	}
	var entry handleEntry
	if got := unsafe.Offsetof(entry.off); got != NativeHandleOffsetOffset {
		t.Fatalf("handle offset field = %d", got)
	}
	if got := unsafe.Offsetof(entry.size); got != NativeHandleSizeOffset {
		t.Fatalf("handle size field = %d", got)
	}
	if got := unsafe.Offsetof(entry.cardSlot); got != NativeHandleCardSlotOffset {
		t.Fatalf("handle card slot field = %d", got)
	}
	if got := unsafe.Offsetof(entry.space); got != NativeHandleSpaceOffset {
		t.Fatalf("handle space field = %d", got)
	}
	if got := unsafe.Offsetof(entry.remembered); got != NativeHandleRememberedOffset {
		t.Fatalf("handle remembered field = %d", got)
	}
	var card objectCard
	if unsafe.Sizeof(card) != NativeObjectCardStride || unsafe.Offsetof(card.handle) != NativeObjectCardHandleOffset || unsafe.Offsetof(card.index) != NativeObjectCardStartOffset || unsafe.Offsetof(card.end) != NativeObjectCardEndOffset || unsafe.Offsetof(card.next) != NativeObjectCardNextOffset {
		t.Fatalf("object card layout changed: size=%d handle=%d start=%d end=%d next=%d", unsafe.Sizeof(card), unsafe.Offsetof(card.handle), unsafe.Offsetof(card.index), unsafe.Offsetof(card.end), unsafe.Offsetof(card.next))
	}
	var space NativeSpaceView
	if unsafe.Sizeof(space) != NativeViewSpaceStride || unsafe.Offsetof(space.Base) != NativeSpaceBaseOffset || unsafe.Offsetof(space.Bytes) != NativeSpaceBytesOffset {
		t.Fatalf("space view layout changed: size=%d base=%d bytes=%d", unsafe.Sizeof(space), unsafe.Offsetof(space.Base), unsafe.Offsetof(space.Bytes))
	}
	var view NativeCollectorView
	checks := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"version", unsafe.Offsetof(view.Version), NativeViewVersionOffset},
		{"stride", unsafe.Offsetof(view.HandleStride), NativeViewHandleStrideOffset},
		{"handles", unsafe.Offsetof(view.Handles), NativeViewHandlesOffset},
		{"handle count", unsafe.Offsetof(view.HandleCount), NativeViewHandleCountOffset},
		{"spaces", unsafe.Offsetof(view.Spaces), NativeViewSpacesOffset},
		{"generation", unsafe.Offsetof(view.RefreshGeneration), NativeViewRefreshGenerationOffset},
		{"object cards", unsafe.Offsetof(view.ObjectCards), NativeViewObjectCardsOffset},
		{"object card count", unsafe.Offsetof(view.ObjectCardCount), NativeViewObjectCardCountOffset},
		{"nursery allocation bytes", unsafe.Offsetof(view.NurseryAllocBytes), NativeViewNurseryAllocBytesOffset},
		{"struct alloc state", unsafe.Offsetof(view.StructAllocState), NativeViewStructAllocStateOffset},
		{"struct alloc epoch", unsafe.Offsetof(view.StructAllocEpoch), NativeViewStructAllocEpochOffset},
		{"nursery bump", unsafe.Offsetof(view.NurseryBump), NativeViewNurseryBumpOffset},
		{"allocation count", unsafe.Offsetof(view.AllocationCount), NativeViewAllocationCountOffset},
		{"nursery object max bytes", unsafe.Offsetof(view.NurseryObjectMaxBytes), NativeViewNurseryObjectMaxBytesOffset},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Fatalf("%s offset = %d, want %d", check.name, check.got, check.want)
		}
	}
	if got := unsafe.Sizeof(view); got != NativeCollectorViewSize {
		t.Fatalf("collector view size = %d, want %d", got, NativeCollectorViewSize)
	}
	var instance NativeInstanceView
	if got := unsafe.Offsetof(instance.keepTypes); got != NativeInstanceViewABISize {
		t.Fatalf("instance view native prefix = %d, want %d", got, NativeInstanceViewABISize)
	}
	if unsafe.Offsetof(instance.Collector) != NativeInstanceViewCollectorOffset ||
		unsafe.Offsetof(instance.LocalTypes) != NativeInstanceViewLocalTypesOffset ||
		unsafe.Offsetof(instance.LocalTypeCount) != NativeInstanceViewLocalTypeCountOffset {
		t.Fatalf("instance view offsets changed")
	}

	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{DisableCollection: true}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	v := c.NativeView()
	if v == nil || v.Version != NativeABIVersion || v.HandleCount != 1 || v.Handles == 0 || v.Spaces[NativeSpaceNursery].Base == 0 || v.NurseryAllocBytes != c.edenBytes() || v.StructAllocState == 0 || v.StructAllocEpoch == 0 || v.NurseryBump == 0 || v.AllocationCount == 0 || v.NurseryObjectMaxBytes != c.cfg.LargeObjectBytes {
		t.Fatalf("initial native view = %+v", v)
	}
	generation := v.RefreshGeneration
	if _, err := c.NewStructDefault(0); err != nil {
		t.Fatal(err)
	}
	if v.HandleCount != 2 || v.RefreshGeneration <= generation {
		t.Fatalf("refreshed native view = %+v, old generation %d", v, generation)
	}
	c.Close()
	for i, space := range v.Spaces {
		if space.Base != 0 || space.Bytes != 0 {
			t.Fatalf("closed native view space %d retains backing: %+v", i, space)
		}
	}
	if v.Handles != 0 || v.HandleCount != 0 || v.ObjectCards != 0 || v.ObjectCardCount != 0 || v.NurseryAllocBytes != 0 || v.StructAllocState != 0 || v.StructAllocEpoch != 0 || v.NurseryBump != 0 || v.AllocationCount != 0 || v.NurseryObjectMaxBytes != 0 {
		t.Fatalf("closed native view retains handles/cards/allocation state: %+v", v)
	}
}

func TestNativeInstanceViewPublishesImmutableTypeMap(t *testing.T) {
	if got := NewNativeInstanceView(nil, []TypeID{1}); got != nil {
		t.Fatalf("nil collector instance view = %+v", got)
	}
	c := newTestCollector(t, Config{})
	localTypes := []TypeID{2, 1, 0}
	view := NewNativeInstanceView(c, localTypes)
	if view == nil || view.Version != NativeABIVersion || view.Collector == 0 || view.LocalTypes == 0 || view.LocalTypeCount != uint32(len(localTypes)) {
		t.Fatalf("native instance view = %+v", view)
	}
	if len(view.keepTypes) != len(localTypes) || &view.keepTypes[0] != &localTypes[0] {
		t.Fatal("native instance view did not retain caller-owned type map")
	}
	empty := NewNativeInstanceView(c, nil)
	if empty == nil || empty.LocalTypes != 0 || empty.LocalTypeCount != 0 || empty.Collector != view.Collector {
		t.Fatalf("empty native instance view = %+v", empty)
	}
}
