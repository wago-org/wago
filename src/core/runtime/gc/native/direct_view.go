// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package gc

import (
	"errors"
	"unsafe"
)

// DirectABIVersion is independent of NativeABIVersion. Only a separately admitted
// direct-offset artifact may interpret these descriptors; they must NEVER alias
// NativeCollectorView/NativeInstanceView, even when basedata reuses one slot.
const DirectABIVersion uint32 = 1

type DirectCollectorView struct {
	Version                                      uint32
	Size                                         uint32
	Arena                                        uintptr
	ArenaBytes                                   uint32
	_                                            uint32
	Starts                                       uintptr
	StartCount                                   uint32
	_                                            uint32
	NurseryStart, NurseryBump, OldStart, OldBump uint32
}

const (
	DirectViewVersionOffset      = 0
	DirectViewSizeOffset         = 4
	DirectViewArenaOffset        = 8
	DirectViewArenaBytesOffset   = 16
	DirectViewStartsOffset       = 24
	DirectViewStartCountOffset   = 32
	DirectViewNurseryStartOffset = 40
	DirectViewNurseryBumpOffset  = 44
	DirectViewOldStartOffset     = 48
	DirectViewOldBumpOffset      = 52
	DirectCollectorViewSize      = 56
)

type DirectInstanceView struct {
	Version        uint32
	Size           uint32
	Collector      uintptr
	LocalTypes     uintptr
	LocalTypeCount uint32
	_              uint32
	keepTypes      []TypeID
}

const (
	DirectInstanceVersionOffset        = 0
	DirectInstanceSizeOffset           = 4
	DirectInstanceCollectorOffset      = 8
	DirectInstanceLocalTypesOffset     = 16
	DirectInstanceLocalTypeCountOffset = 24
	DirectInstanceViewABISize          = 32
)

func validateDirectViewLayout() error {
	var c DirectCollectorView
	var i DirectInstanceView
	if unsafe.Sizeof(uintptr(0)) != 8 || unsafe.Sizeof(bool(false)) != 1 || unsafe.Sizeof(c) != DirectCollectorViewSize ||
		unsafe.Offsetof(c.Version) != DirectViewVersionOffset || unsafe.Offsetof(c.Size) != DirectViewSizeOffset || unsafe.Offsetof(c.Arena) != DirectViewArenaOffset || unsafe.Offsetof(c.ArenaBytes) != DirectViewArenaBytesOffset || unsafe.Offsetof(c.Starts) != DirectViewStartsOffset || unsafe.Offsetof(c.StartCount) != DirectViewStartCountOffset || unsafe.Offsetof(c.NurseryStart) != DirectViewNurseryStartOffset || unsafe.Offsetof(c.NurseryBump) != DirectViewNurseryBumpOffset || unsafe.Offsetof(c.OldStart) != DirectViewOldStartOffset || unsafe.Offsetof(c.OldBump) != DirectViewOldBumpOffset ||
		unsafe.Offsetof(i.Version) != DirectInstanceVersionOffset || unsafe.Offsetof(i.Size) != DirectInstanceSizeOffset || unsafe.Offsetof(i.Collector) != DirectInstanceCollectorOffset || unsafe.Offsetof(i.LocalTypes) != DirectInstanceLocalTypesOffset || unsafe.Offsetof(i.LocalTypeCount) != DirectInstanceLocalTypeCountOffset || unsafe.Offsetof(i.keepTypes) != DirectInstanceViewABISize {
		return errors.New("gc: unsupported direct view layout")
	}
	return nil
}

// refreshDirectView preserves descriptor identity. The owner serializes native
// execution with every mutation; publication is not an atomic concurrency API.
func (c *Collector) refreshDirectView() {
	if c == nil || c.directView == nil {
		return
	}
	v := c.directView
	if c.closed || c.direct == nil {
		*v = DirectCollectorView{Version: DirectABIVersion, Size: DirectCollectorViewSize}
		return
	}
	x := c.direct.ReadView()
	*v = DirectCollectorView{Version: DirectABIVersion, Size: DirectCollectorViewSize, Arena: x.Arena, ArenaBytes: x.ArenaBytes, Starts: x.Starts, StartCount: x.StartCount, NurseryStart: x.NurseryStart, NurseryBump: x.NurseryBump, OldStart: x.OldStart, OldBump: x.OldBump}
}
func (c *Collector) DirectView() *DirectCollectorView {
	if c == nil || c.closed || c.direct == nil {
		return nil
	}
	if c.directView == nil {
		c.directView = &DirectCollectorView{}
	}
	c.refreshDirectView()
	return c.directView
}

// BuildDirectInstanceView snapshots the canonical type map. Integer addresses
// are valid only while the owning instance/collector remains alive.
func BuildDirectInstanceView(c *Collector, localTypes []TypeID) (*DirectInstanceView, error) {
	if e := validateDirectViewLayout(); e != nil {
		return nil, e
	}
	v := c.DirectView()
	if v == nil {
		return nil, errors.New("gc: direct collector unavailable")
	}
	if uint64(len(localTypes)) > uint64(^uint32(0)) {
		return nil, errors.New("gc: direct local type map too large")
	}
	for _, id := range localTypes {
		if uint64(id) >= uint64(len(c.types)) {
			return nil, errors.New("gc: invalid direct local type map")
		}
	}
	types := append([]TypeID(nil), localTypes...)
	return &DirectInstanceView{Version: DirectABIVersion, Size: DirectInstanceViewABISize, Collector: uintptr(unsafe.Pointer(v)), LocalTypes: sliceData(types), LocalTypeCount: uint32(len(types)), keepTypes: types}, nil
}

// ValidateDirectInstanceView checks the separate direct ABI before publishing
// the instance address to admitted generated code. Descriptor fields are trusted
// read-only thereafter; runtime ownership must retain both instance and collector.
func ValidateDirectInstanceView(v *DirectInstanceView, c *Collector) error {
	if e := validateDirectViewLayout(); e != nil {
		return e
	}
	if v == nil || c == nil || c.closed || c.direct == nil || c.directView == nil {
		return errors.New("gc: unavailable direct instance view")
	}
	cv := c.directView
	if v.Version != DirectABIVersion || v.Size != DirectInstanceViewABISize || v.Collector != uintptr(unsafe.Pointer(cv)) || cv.Version != DirectABIVersion || cv.Size != DirectCollectorViewSize || cv.Arena == 0 || cv.Starts == 0 {
		return errors.New("gc: invalid direct instance ABI")
	}
	if uint64(v.LocalTypeCount) != uint64(len(v.keepTypes)) || v.LocalTypes != sliceData(v.keepTypes) {
		return errors.New("gc: invalid direct canonical type backing")
	}
	for _, id := range v.keepTypes {
		if uint64(id) >= uint64(len(c.types)) {
			return errors.New("gc: invalid direct canonical type")
		}
	}
	return nil
}
