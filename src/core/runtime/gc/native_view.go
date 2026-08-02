package gc

import "unsafe"

// NativeABIVersion identifies the checked collector metadata layout consumed by
// generated code. Native code must compare this value before following any
// pointer. The view is refreshed in place whenever a growable backing slice can
// relocate; callers must still serialize access with collector mutation.
const NativeABIVersion uint32 = 3

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
	NativeObjectCardStride            = 12
	NativeObjectCardHandleOffset      = 0
	NativeObjectCardStartOffset       = 4
	NativeObjectCardEndOffset         = 8
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
	Version           uint32
	HandleStride      uint32
	Handles           uintptr
	HandleCount       uint32
	_                 uint32
	Spaces            [NativeSpaceCount]NativeSpaceView
	RefreshGeneration uint64
	ObjectCards       uintptr
	ObjectCardCount   uint32
	_                 uint32
	StructAllocState  uintptr
	StructAllocEpoch  uintptr
	NurseryBump       uintptr
	AllocationCount   uintptr
}

const (
	NativeViewVersionOffset           = 0
	NativeViewHandleStrideOffset      = 4
	NativeViewHandlesOffset           = 8
	NativeViewHandleCountOffset       = 16
	NativeViewSpacesOffset            = 24
	NativeViewSpaceStride             = 16
	NativeSpaceBaseOffset             = 0
	NativeSpaceBytesOffset            = 8
	NativeViewRefreshGenerationOffset = 104
	NativeViewObjectCardsOffset       = 112
	NativeViewObjectCardCountOffset   = 120
	NativeViewStructAllocStateOffset  = 128
	NativeViewStructAllocEpochOffset  = 136
	NativeViewNurseryBumpOffset       = 144
	NativeViewAllocationCountOffset   = 152
	NativeCollectorViewSize           = 160
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
	if c.closed {
		v.StructAllocState = 0
		v.StructAllocEpoch = 0
		v.NurseryBump = 0
		v.AllocationCount = 0
	} else {
		v.StructAllocState = uintptr(unsafe.Pointer(&c.nativeStructAlloc))
		v.StructAllocEpoch = uintptr(unsafe.Pointer(&c.nativeAllocEpoch))
		v.NurseryBump = uintptr(unsafe.Pointer(&c.nurseryBump))
		v.AllocationCount = uintptr(unsafe.Pointer(&c.stats.Allocations))
	}
	v.RefreshGeneration++
}

// refreshNativeCards republishes the relocatable object-card backing after
// append/remove/clear operations. In-place interval widening needs no refresh.
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
	if c == nil || c.nativeView == nil {
		return nil
	}
	return &NativeInstanceView{
		Version:        NativeABIVersion,
		Collector:      uintptr(unsafe.Pointer(c.nativeView)),
		LocalTypes:     sliceData(localTypes),
		LocalTypeCount: uint32(len(localTypes)),
		keepTypes:      localTypes,
	}
}
