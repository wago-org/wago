package wago

import "unsafe"

// Two ABI words retain a slice's pointer, length and capacity. Ordinary host
// frames fit in 32-bit counts; oversized views retain their original header in
// a separately rooted descriptor rather than truncating or imposing a limit.
type hostCallSlots struct {
	data   *uint64
	counts uint64
}

type largeHostCallSlots struct{ slots []uint64 }

const largeHostCallSlotsTag = ^uint64(0)

func compactHostSlots(slots []uint64) hostCallSlots {
	// A valid Go slice has len <= cap. Reserve the all-ones encoding and keep
	// the common constructor below Go's inlining budget.
	if uint64(cap(slots)) >= uint64(1<<32-1) {
		return compactLargeHostSlots(slots)
	}
	return hostCallSlots{data: unsafe.SliceData(slots), counts: uint64(len(slots)) | uint64(cap(slots))<<32}
}

func compactLargeHostSlots(slots []uint64) hostCallSlots {
	wide := &largeHostCallSlots{slots: slots}
	return hostCallSlots{data: (*uint64)(unsafe.Pointer(wide)), counts: largeHostCallSlotsTag}
}

func (s hostCallSlots) len() int {
	if s.counts == largeHostCallSlotsTag {
		return (*largeHostCallSlots)(unsafe.Pointer(s.data)).slotsLen()
	}
	return int(uint32(s.counts))
}
func (s *largeHostCallSlots) slotsLen() int { return len(s.slots) }

func (s hostCallSlots) slice() []uint64 {
	if s.counts == largeHostCallSlotsTag {
		return (*largeHostCallSlots)(unsafe.Pointer(s.data)).slots
	}
	header := struct {
		data     *uint64
		len, cap int
	}{s.data, int(uint32(s.counts)), int(uint32(s.counts >> 32))}
	return *(*[]uint64)(unsafe.Pointer(&header))
}
