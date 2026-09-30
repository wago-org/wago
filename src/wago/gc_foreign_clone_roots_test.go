package wago

import (
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

type foreignCloneRootSink struct {
	refs    [2]gc.Ref
	classes [2]gc.RootClass
	count   int
	stop    bool
}

func (s *foreignCloneRootSink) VisitRootRef(ref gc.Ref) bool {
	s.refs[s.count] = ref
	s.count++
	return !s.stop
}
func (s *foreignCloneRootSink) VisitClassifiedRootRef(class gc.RootClass, ref gc.Ref) bool {
	s.classes[s.count] = class
	return s.VisitRootRef(ref)
}

func TestForeignCloneRootsEnumeratesNormalAndTemporaryRoots(t *testing.T) {
	frame := make([]byte, 16)
	// Native frame addresses must remain stable across Go stack growth.
	t.Cleanup(func() { runtime.KeepAlive(frame) })
	binary.LittleEndian.PutUint64(frame, 7)
	normal := gcNativeFrameRoots{base: uintptr(unsafe.Pointer(&frame[0])), offsets: []uint32{0}}
	refs := gc.RefSliceRoots{11}
	roots := gcForeignCloneRoots{normal: &normal, refs: refs}
	sink := foreignCloneRootSink{}
	if !roots.RangeRootRefs(&sink) || sink.count != 2 || sink.refs != [2]gc.Ref{7, 11} {
		t.Fatalf("direct roots = %+v", sink)
	}
	sink = foreignCloneRootSink{}
	if !roots.RangeClassifiedRootRefs(&sink) || sink.count != 2 || sink.refs != [2]gc.Ref{7, 11} || sink.classes != [2]gc.RootClass{gc.RootNativeFrame, gc.RootSnapshotTemporary} {
		t.Fatalf("classified roots = %+v", sink)
	}
	sink = foreignCloneRootSink{stop: true}
	if roots.RangeRootRefs(&sink) || sink.count != 1 {
		t.Fatalf("early stop = %+v", sink)
	}
	seen := 0
	roots.RangeRoots(func(slot gc.RootSlot) bool { seen++; slot.SetRef(slot.GetRef() + 2); return true })
	if seen != 2 || binary.LittleEndian.Uint64(frame) != 9 || refs[0] != 13 {
		t.Fatalf("mutable roots = %d, %d, %d", seen, binary.LittleEndian.Uint64(frame), refs[0])
	}
	seen = 0
	roots.RangeRoots(func(gc.RootSlot) bool { seen++; return false })
	if seen != 1 {
		t.Fatalf("mutable early stop = %d", seen)
	}
	runtime.KeepAlive(frame)
}
