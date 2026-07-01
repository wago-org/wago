package gc

import (
	"fmt"
	"unsafe"
)

// ObjHeader is the stable guest-object header stored in the byte heap before
// every payload. It is represented here for layout documentation/tests; heap
// memory stores the fields little-endian, not as Go heap objects.
type ObjHeader struct {
	TypeID uint32
	Size   uint32 // total object size including header
	Aux    uint32 // array length, or forwarding metadata during copying
	Flags  uint32 // generation/color/age/pointer-free/forwarding bits
}

const (
	FlagPointerFree uint32 = 1 << iota
	FlagForwarded
	FlagMarked
	FlagOld
	FlagLarge
)

const HeaderSize = uint32(unsafe.Sizeof(ObjHeader{}))
const PayloadOffset = HeaderSize

func Align8(v uint32) uint32 { return align(v, 8) }
func StructSize(d TypeDesc) (uint32, error) {
	if d.Kind != KindStruct {
		return 0, fmt.Errorf("gc: type %d is not a struct", d.ID)
	}
	total := uint64(HeaderSize) + uint64(d.Size)
	if total > uint64(^uint32(0)-7) {
		return 0, fmt.Errorf("gc: struct size overflow")
	}
	return Align8(uint32(total)), nil
}
func ArraySize(d TypeDesc, length uint32) (uint32, error) {
	if d.Kind != KindArray {
		return 0, fmt.Errorf("gc: type %d is not an array", d.ID)
	}
	payload := uint64(d.ElemSize) * uint64(length)
	total := uint64(HeaderSize) + payload
	if total > uint64(^uint32(0)-7) {
		return 0, fmt.Errorf("gc: array size overflow")
	}
	return Align8(uint32(total)), nil
}
