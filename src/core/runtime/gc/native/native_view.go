package gc

import (
	"fmt"
	"unsafe"
)

// NativeABIVersion identifies the checked collector metadata layout consumed by
// generated code. Artifact loading and instantiation must validate it before
// publishing the basedata pointer; generated operations then trust that immutable
// contract while reloading every mutable backing pointer/count they consume. The
// view is refreshed in place whenever growable backing can relocate, and callers
// must still serialize access with collector mutation.
const NativeABIVersion uint32 = 1

// Native handle-entry layout. These constants are part of NativeABIVersion and
// are verified against handleEntry below.
const (
	NativeHandleStride                = 20
	NativeHandleOffsetOffset          = 0
	NativeHandleSizeOffset            = 4
	NativeHandleCardSlotOffset        = 12
	NativeHandleSpaceOffset           = 18
	NativeHandleRememberedOffset      = 19
	NativeSpaceFree              byte = byte(spaceFree)
	NativeSpaceNursery           byte = byte(spaceNursery)
	NativeSpaceOld               byte = byte(spaceOld)
	NativeSpaceLarge             byte = byte(spaceLarge)
	NativeSpaceTiny              byte = byte(spaceTiny)
	NativeSpaceCount                  = 5
	NativeObjectCardStride            = 16
	NativeObjectCardHandleOffset      = 0
	NativeObjectCardStartOffset       = 4
	NativeObjectCardEndOffset         = 8
	NativeObjectCardNextOffset        = 12
)

// NativeSpaceView names one collector heap backing. Space zero deliberately
// remains empty so a handle's native space byte indexes this array directly.
type NativeSpaceView struct {
	Base  uintptr
	Bytes uint32
	_     uint32
}

// NativeCollectorView is a stable, versioned descriptor for the collector's
// relocatable handle table and fixed heap spaces. The Collector owns this object
// for its complete lifetime, so generated code may cache only the view pointer;
// it must reload all backing pointers and lengths for each access.
//
// All fields use naturally aligned fixed-width words so their offsets are the
// same on amd64 and arm64. Keep offset constants below in sync.
type NativeCollectorView struct {
	Version               uint32
	HandleStride          uint32
	Handles               uintptr
	HandleCount           uint32
	_                     uint32
	Spaces                [NativeSpaceCount]NativeSpaceView
	RefreshGeneration     uint64
	ObjectCards           uintptr
	ObjectCardCount       uint32
	NurseryAllocBytes     uint32
	StructAllocState      uintptr
	StructAllocEpoch      uintptr
	NurseryBump           uintptr
	AllocationCount       uintptr
	NurseryObjectMaxBytes uint32
	_                     uint32
	SubtypeIntervals      uintptr
	SubtypeIntervalCount  uint32
	_                     uint32
}

const (
	NativeViewVersionOffset               = 0
	NativeViewHandleStrideOffset          = 4
	NativeViewHandlesOffset               = 8
	NativeViewHandleCountOffset           = 16
	NativeViewSpacesOffset                = 24
	NativeViewSpaceStride                 = 16
	NativeSpaceBaseOffset                 = 0
	NativeSpaceBytesOffset                = 8
	NativeViewRefreshGenerationOffset     = 104
	NativeViewObjectCardsOffset           = 112
	NativeViewObjectCardCountOffset       = 120
	NativeViewNurseryAllocBytesOffset     = 124
	NativeViewStructAllocStateOffset      = 128
	NativeViewStructAllocEpochOffset      = 136
	NativeViewNurseryBumpOffset           = 144
	NativeViewAllocationCountOffset       = 152
	NativeViewNurseryObjectMaxBytesOffset = 160
	NativeViewSubtypeIntervalsOffset      = 168
	NativeViewSubtypeIntervalCountOffset  = 176
	NativeCollectorViewSize               = 184
)

// NativeInstanceView adds the immutable module-local to canonical-domain type
// map needed by generated type checks. Collector points at the shared view above.
type NativeInstanceView struct {
	Version        uint32
	_              uint32
	Collector      uintptr
	LocalTypes     uintptr
	LocalTypeCount uint32
	_              uint32

	// keepTypes is outside the native ABI prefix. It gives Go's collector a typed
	// reference to the immutable backing array whose address LocalTypes publishes.
	keepTypes []TypeID
}

const (
	NativeInstanceViewVersionOffset        = 0
	NativeInstanceViewCollectorOffset      = 8
	NativeInstanceViewLocalTypesOffset     = 16
	NativeInstanceViewLocalTypeCountOffset = 24
	NativeInstanceViewABISize              = 32
)

func sliceData[T any](s []T) uintptr {
	if len(s) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&s[0]))
}

// ValidateNativeABI verifies the Go layouts behind the native collector ABI.
// It belongs at collector/artifact construction boundaries, never in generated
// hot paths. Any mismatch is fail-closed before native code can follow a field.
func ValidateNativeABI() error {
	var entry handleEntry
	if unsafe.Sizeof(entry) != NativeHandleStride ||
		unsafe.Offsetof(entry.off) != NativeHandleOffsetOffset ||
		unsafe.Offsetof(entry.size) != NativeHandleSizeOffset ||
		unsafe.Offsetof(entry.cardSlot) != NativeHandleCardSlotOffset ||
		unsafe.Offsetof(entry.space) != NativeHandleSpaceOffset ||
		unsafe.Offsetof(entry.remembered) != NativeHandleRememberedOffset {
		return fmt.Errorf("gc: native handle ABI layout mismatch")
	}
	var card objectCard
	if unsafe.Sizeof(card) != NativeObjectCardStride ||
		unsafe.Offsetof(card.handle) != NativeObjectCardHandleOffset ||
		unsafe.Offsetof(card.index) != NativeObjectCardStartOffset ||
		unsafe.Offsetof(card.end) != NativeObjectCardEndOffset ||
		unsafe.Offsetof(card.next) != NativeObjectCardNextOffset {
		return fmt.Errorf("gc: native object-card ABI layout mismatch")
	}
	var alloc nativeStructAllocState
	if unsafe.Sizeof(alloc) != NativeStructAllocStateSize ||
		unsafe.Offsetof(alloc.Epoch) != NativeStructAllocEpochOffset ||
		unsafe.Offsetof(alloc.Cursor) != NativeStructAllocCursorOffset ||
		unsafe.Offsetof(alloc.Count) != NativeStructAllocCountOffset ||
		unsafe.Offsetof(alloc.HandleBase) != NativeStructAllocHandleBaseOffset ||
		unsafe.Offsetof(alloc.ChunkStart) != NativeStructAllocChunkStartOffset ||
		unsafe.Offsetof(alloc.ChunkCursor) != NativeStructAllocChunkCursorOffset ||
		unsafe.Offsetof(alloc.ChunkEnd) != NativeStructAllocChunkEndOffset ||
		unsafe.Offsetof(alloc.ChunkBump) != NativeStructAllocChunkBumpOffset ||
		unsafe.Offsetof(alloc.Handles) != NativeStructAllocHandlesOffset {
		return fmt.Errorf("gc: native allocation-state ABI layout mismatch")
	}
	var space NativeSpaceView
	if unsafe.Sizeof(space) != NativeViewSpaceStride ||
		unsafe.Offsetof(space.Base) != NativeSpaceBaseOffset ||
		unsafe.Offsetof(space.Bytes) != NativeSpaceBytesOffset {
		return fmt.Errorf("gc: native space ABI layout mismatch")
	}
	var view NativeCollectorView
	if unsafe.Sizeof(view) != NativeCollectorViewSize ||
		unsafe.Offsetof(view.Version) != NativeViewVersionOffset ||
		unsafe.Offsetof(view.HandleStride) != NativeViewHandleStrideOffset ||
		unsafe.Offsetof(view.Handles) != NativeViewHandlesOffset ||
		unsafe.Offsetof(view.HandleCount) != NativeViewHandleCountOffset ||
		unsafe.Offsetof(view.Spaces) != NativeViewSpacesOffset ||
		unsafe.Offsetof(view.RefreshGeneration) != NativeViewRefreshGenerationOffset ||
		unsafe.Offsetof(view.ObjectCards) != NativeViewObjectCardsOffset ||
		unsafe.Offsetof(view.ObjectCardCount) != NativeViewObjectCardCountOffset ||
		unsafe.Offsetof(view.NurseryAllocBytes) != NativeViewNurseryAllocBytesOffset ||
		unsafe.Offsetof(view.StructAllocState) != NativeViewStructAllocStateOffset ||
		unsafe.Offsetof(view.StructAllocEpoch) != NativeViewStructAllocEpochOffset ||
		unsafe.Offsetof(view.NurseryBump) != NativeViewNurseryBumpOffset ||
		unsafe.Offsetof(view.AllocationCount) != NativeViewAllocationCountOffset ||
		unsafe.Offsetof(view.NurseryObjectMaxBytes) != NativeViewNurseryObjectMaxBytesOffset ||
		unsafe.Offsetof(view.SubtypeIntervals) != NativeViewSubtypeIntervalsOffset ||
		unsafe.Offsetof(view.SubtypeIntervalCount) != NativeViewSubtypeIntervalCountOffset {
		return fmt.Errorf("gc: native collector ABI layout mismatch")
	}
	var instance NativeInstanceView
	if unsafe.Offsetof(instance.keepTypes) != NativeInstanceViewABISize ||
		unsafe.Offsetof(instance.Version) != NativeInstanceViewVersionOffset ||
		unsafe.Offsetof(instance.Collector) != NativeInstanceViewCollectorOffset ||
		unsafe.Offsetof(instance.LocalTypes) != NativeInstanceViewLocalTypesOffset ||
		unsafe.Offsetof(instance.LocalTypeCount) != NativeInstanceViewLocalTypeCountOffset {
		return fmt.Errorf("gc: native instance ABI layout mismatch")
	}
	return nil
}

// ValidateNativeInstanceView proves the immutable per-instance/native collector
// contract before native execution. Mutable handle, heap, card, and nursery
// pointers are intentionally not cached or validated here; generated code must
// continue reloading and semantically validating them at each access.
func ValidateNativeInstanceView(view *NativeInstanceView, collector *Collector, localTypeCount uint32) error {
	if err := ValidateNativeABI(); err != nil {
		return err
	}
	if view == nil {
		return fmt.Errorf("gc: native instance view is missing")
	}
	if collector == nil || collector.closed || collector.nativeView == nil {
		return fmt.Errorf("gc: native collector view is unavailable")
	}
	if view.Version != NativeABIVersion {
		return fmt.Errorf("gc: native instance ABI version %d unsupported (want %d)", view.Version, NativeABIVersion)
	}
	if view.LocalTypeCount != localTypeCount || uint32(len(view.keepTypes)) != localTypeCount {
		return fmt.Errorf("gc: native local type map count %d does not match module count %d", view.LocalTypeCount, localTypeCount)
	}
	if view.LocalTypes != sliceData(view.keepTypes) {
		return fmt.Errorf("gc: native local type map pointer does not match immutable backing")
	}
	collectorPointer := uintptr(unsafe.Pointer(collector.nativeView))
	if view.Collector != collectorPointer {
		return fmt.Errorf("gc: native collector view pointer does not match instance collector")
	}
	if collector.nativeView.Version != NativeABIVersion {
		return fmt.Errorf("gc: native collector ABI version %d unsupported (want %d)", collector.nativeView.Version, NativeABIVersion)
	}
	if collector.nativeView.HandleStride != NativeHandleStride {
		return fmt.Errorf("gc: native handle stride %d unsupported (want %d)", collector.nativeView.HandleStride, NativeHandleStride)
	}
	if collector.nativeView.SubtypeIntervalCount != uint32(len(collector.subtypeIntervals)) || collector.nativeView.SubtypeIntervals != sliceData(collector.subtypeIntervals) {
		return fmt.Errorf("gc: native subtype interval view does not match immutable collector backing")
	}
	return nil
}

func (c *Collector) initNativeView() {
	if c.nativeView == nil {
		c.nativeView = &NativeCollectorView{}
	}
	c.refreshNativeView()
}

func (c *Collector) refreshNativeView() {
	if c == nil || c.nativeView == nil {
		return
	}
	v := c.nativeView
	v.Version = NativeABIVersion
	v.HandleStride = NativeHandleStride
	v.Handles = sliceData(c.handles)
	v.HandleCount = uint32(len(c.handles))
	v.Spaces[NativeSpaceNursery] = NativeSpaceView{Base: sliceData(c.nursery), Bytes: uint32(len(c.nursery))}
	v.Spaces[NativeSpaceOld] = NativeSpaceView{Base: sliceData(c.throughput.mem), Bytes: uint32(len(c.throughput.mem))}
	v.Spaces[NativeSpaceLarge] = v.Spaces[NativeSpaceOld]
	v.Spaces[NativeSpaceTiny] = NativeSpaceView{Base: sliceData(c.tiny.mem), Bytes: uint32(len(c.tiny.mem))}
	v.ObjectCards = sliceData(c.objectCards)
	v.ObjectCardCount = uint32(len(c.objectCards))
	v.SubtypeIntervals = sliceData(c.subtypeIntervals)
	v.SubtypeIntervalCount = uint32(len(c.subtypeIntervals))
	if c.closed {
		v.NurseryAllocBytes = 0
		v.StructAllocState = 0
		v.StructAllocEpoch = 0
		v.NurseryBump = 0
		v.AllocationCount = 0
		v.NurseryObjectMaxBytes = 0
	} else {
		v.NurseryAllocBytes = c.edenBytes()
		v.StructAllocState = uintptr(unsafe.Pointer(&c.nativeStructAlloc))
		v.StructAllocEpoch = uintptr(unsafe.Pointer(&c.nativeAllocEpoch))
		v.NurseryBump = uintptr(unsafe.Pointer(&c.nurseryBump))
		v.AllocationCount = uintptr(unsafe.Pointer(&c.stats.Allocations))
		v.NurseryObjectMaxBytes = c.cfg.LargeObjectBytes
	}
	v.RefreshGeneration++
}

// refreshNativeCards republishes the relocatable object-card backing after
// append/remove/clear operations. In-place range coalescing needs no refresh.
func (c *Collector) refreshNativeCards() {
	if c == nil || c.nativeView == nil {
		return
	}
	v := c.nativeView
	v.ObjectCards = sliceData(c.objectCards)
	v.ObjectCardCount = uint32(len(c.objectCards))
	v.RefreshGeneration++
}

// refreshNativeHandles updates only allocation-mutated handle metadata. A
// nursery allocation cannot relocate any heap-space backing, so rewriting all
// five space descriptors on every hot allocation is redundant. Collection,
// large-space growth, and Tiny allocation continue to publish the full view.
func (c *Collector) refreshNativeHandles() {
	if c == nil || c.nativeView == nil {
		return
	}
	v := c.nativeView
	v.Handles = sliceData(c.handles)
	v.HandleCount = uint32(len(c.handles))
	v.RefreshGeneration++
}

// NativeView returns the collector-owned stable metadata view. The returned
// pointer becomes invalid only after Collector.Close; Close first zeros all
// backing pointers and increments RefreshGeneration.
func (c *Collector) NativeView() *NativeCollectorView {
	if c == nil {
		return nil
	}
	return c.nativeView
}

// NewNativeInstanceView constructs an instance-owned immutable type-map view.
// localTypes must remain alive and unchanged for the view's lifetime.
func NewNativeInstanceView(c *Collector, localTypes []TypeID) *NativeInstanceView {
	view, err := BuildNativeInstanceView(c, localTypes)
	if err != nil {
		return nil
	}
	return view
}

// BuildNativeInstanceView constructs and validates the immutable instance view.
// Callers that install the pointer into basedata should use this error-returning
// form so malformed internal state is rejected before native execution.
func BuildNativeInstanceView(c *Collector, localTypes []TypeID) (*NativeInstanceView, error) {
	if c == nil || c.nativeView == nil {
		return nil, fmt.Errorf("gc: native collector view is unavailable")
	}
	view := &NativeInstanceView{
		Version:        NativeABIVersion,
		Collector:      uintptr(unsafe.Pointer(c.nativeView)),
		LocalTypes:     sliceData(localTypes),
		LocalTypeCount: uint32(len(localTypes)),
		keepTypes:      localTypes,
	}
	if err := ValidateNativeInstanceView(view, c, uint32(len(localTypes))); err != nil {
		return nil, err
	}
	return view, nil
}
