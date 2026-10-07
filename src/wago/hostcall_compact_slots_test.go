package wago

import (
	"reflect"
	goruntime "runtime"
	"testing"
	"unsafe"
)

func TestCompactHostSlotsPreserveSliceIdentity(t *testing.T) {
	backing := []uint64{11, 22, 33, 44}
	for _, slots := range [][]uint64{nil, {}, backing[:0], backing[:2], backing[1:3:3]} {
		view := compactHostSlots(slots)
		got := view.slice()
		if len(got) != len(slots) || cap(got) != cap(slots) || (got == nil) != (slots == nil) || unsafe.SliceData(got) != unsafe.SliceData(slots) || view.len() != len(slots) {
			t.Fatalf("shape changed: len/cap=%d/%d → %d/%d", len(slots), cap(slots), len(got), cap(got))
		}
		if len(got) != 0 {
			before := slots[0]
			got[0] = 99
			if slots[0] != 99 {
				t.Fatal("lost writable alias")
			}
			got[0] = before
		}
	}
	if unsafe.Sizeof(HostCall{}) != 48 {
		t.Fatalf("HostCall size=%d", unsafe.Sizeof(HostCall{}))
	}
	if reflect.TypeOf(HostCall{}).Comparable() {
		t.Fatal("HostCall became comparable")
	}
}

func TestCompactHostSlotsOversizedHeader(t *testing.T) {
	// Only the first backing element is accessed. A synthetic header exercises
	// large-count preservation without allocating a multi-gigabyte array.
	backing := new(uint64)
	*backing = 71
	for _, shape := range [][2]int{{1, 1 << 32}, {1 << 32, 1 << 32}, {1<<32 - 1, 1<<32 - 1}} {
		header := struct {
			data     *uint64
			len, cap int
		}{backing, shape[0], shape[1]}
		slots := *(*[]uint64)(unsafe.Pointer(&header))
		view := compactHostSlots(slots)
		if view.counts != largeHostCallSlotsTag {
			t.Fatal("missing large descriptor")
		}
		slots = nil
		goruntime.GC()
		got := view.slice()
		if len(got) != shape[0] || cap(got) != shape[1] || got[0] != 71 || view.len() != shape[0] {
			t.Fatal("large view changed")
		}
		got[0] = 72
		if *backing != 72 {
			t.Fatal("large view lost alias")
		}
		got[0] = 71
		call := HostCall{params: view, results: view, sig: &FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}}}
		if call.I32(0) != 71 {
			t.Fatal("large indexed read")
		}
		call.SetI32(0, 73)
		if *backing != 73 {
			t.Fatal("large indexed write")
		}
		call.SetRawResult(0, 71, 0)
		lo, hi := call.RawParam(0)
		if lo != 71 || hi != 0 {
			t.Fatal("large raw access")
		}
		goruntime.KeepAlive(view)
	}
}
