package gc

import (
	"errors"
	"unsafe"
)

// CompactNativeABIVersion describes only checked compact reads. It is neither
// DirectABIVersion nor the helper compiler receipt. No native allocation exists.
// Version1 certifies Layout16 only: object starts are16-aligned, struct payload
// begins at alignUp(4, descriptor alignment), arrays at alignUp(8, alignment).
// Any future Layout8 factory must reject this ABI or issue a distinct version.
const CompactNativeABIVersion uint32 = 1

type CompactCollectorView struct {
	Version, Size                                uint32
	Arena                                        uintptr
	ArenaBytes                                   uint32
	_                                            uint32
	Starts                                       uintptr
	StartCount                                   uint32
	_                                            uint32
	NurseryStart, NurseryBump, OldStart, OldBump uint32
}

const (
	CompactViewVersionOffset      = 0
	CompactViewSizeOffset         = 4
	CompactViewArenaOffset        = 8
	CompactViewArenaBytesOffset   = 16
	CompactViewStartsOffset       = 24
	CompactViewStartCountOffset   = 32
	CompactViewNurseryStartOffset = 40
	CompactViewNurseryBumpOffset  = 44
	CompactViewOldStartOffset     = 48
	CompactViewOldBumpOffset      = 52
	CompactCollectorViewSize      = 56
)

type CompactInstanceView struct {
	Version, Size  uint32
	Collector      uintptr
	LocalTypes     uintptr
	LocalTypeCount uint32
	_              uint32
	keepTypes      []TypeID
}

const (
	CompactInstanceVersionOffset        = 0
	CompactInstanceSizeOffset           = 4
	CompactInstanceCollectorOffset      = 8
	CompactInstanceLocalTypesOffset     = 16
	CompactInstanceLocalTypeCountOffset = 24
	CompactInstanceViewABISize          = 32
)

func validateCompactViewLayout() error {
	var c CompactCollectorView
	var i CompactInstanceView
	if unsafe.Sizeof(uintptr(0)) != 8 || unsafe.Sizeof(bool(false)) != 1 || unsafe.Sizeof(c) != CompactCollectorViewSize ||
		unsafe.Offsetof(c.Version) != CompactViewVersionOffset || unsafe.Offsetof(c.Size) != CompactViewSizeOffset || unsafe.Offsetof(c.Arena) != CompactViewArenaOffset || unsafe.Offsetof(c.ArenaBytes) != CompactViewArenaBytesOffset || unsafe.Offsetof(c.Starts) != CompactViewStartsOffset || unsafe.Offsetof(c.StartCount) != CompactViewStartCountOffset || unsafe.Offsetof(c.NurseryStart) != CompactViewNurseryStartOffset || unsafe.Offsetof(c.NurseryBump) != CompactViewNurseryBumpOffset || unsafe.Offsetof(c.OldStart) != CompactViewOldStartOffset || unsafe.Offsetof(c.OldBump) != CompactViewOldBumpOffset ||
		unsafe.Offsetof(i.Version) != CompactInstanceVersionOffset || unsafe.Offsetof(i.Size) != CompactInstanceSizeOffset || unsafe.Offsetof(i.Collector) != CompactInstanceCollectorOffset || unsafe.Offsetof(i.LocalTypes) != CompactInstanceLocalTypesOffset || unsafe.Offsetof(i.LocalTypeCount) != CompactInstanceLocalTypeCountOffset || unsafe.Offsetof(i.keepTypes) != CompactInstanceViewABISize {
		return errors.New("gc: unsupported compact read view layout")
	}
	return nil
}

// BuildCompactInstanceView owns an immutable copy of the canonical type map.
// The runtime must retain this view and its collector while native code runs.
func BuildCompactInstanceView(c *Collector, localTypes []TypeID) (*CompactInstanceView, error) {
	if e := validateCompactViewLayout(); e != nil {
		return nil, e
	}
	v := c.CompactView()
	if v == nil {
		return nil, errors.New("gc: compact read collector unavailable")
	}
	if uint64(len(localTypes)) > uint64(^uint32(0)) {
		return nil, errors.New("gc: compact map too large")
	}
	for _, id := range localTypes {
		if uint64(id) >= uint64(len(c.types)) {
			return nil, errors.New("gc: invalid compact canonical type")
		}
	}
	ids := append([]TypeID(nil), localTypes...)
	return &CompactInstanceView{Version: CompactNativeABIVersion, Size: CompactInstanceViewABISize, Collector: uintptr(unsafe.Pointer(v)), LocalTypes: sliceData(ids), LocalTypeCount: uint32(len(ids)), keepTypes: ids}, nil
}
func ValidateCompactInstanceView(v *CompactInstanceView, c *Collector) error {
	if e := validateCompactViewLayout(); e != nil {
		return e
	}
	cv := c.currentCompactView()
	if v == nil || cv == nil || c.closed || c.direct == nil {
		return errors.New("gc: compact read view unavailable")
	}
	if v.Version != CompactNativeABIVersion || v.Size != CompactInstanceViewABISize || v.Collector != uintptr(unsafe.Pointer(cv)) || cv.Arena == 0 || cv.Starts == 0 || *cv != c.compactReadSnapshot() {
		return errors.New("gc: invalid compact collector backing")
	}
	if uint64(v.LocalTypeCount) != uint64(len(v.keepTypes)) || v.LocalTypes != sliceData(v.keepTypes) {
		return errors.New("gc: invalid compact canonical backing")
	}
	for _, id := range v.keepTypes {
		if uint64(id) >= uint64(len(c.types)) {
			return errors.New("gc: invalid compact canonical type")
		}
	}
	return nil
}
