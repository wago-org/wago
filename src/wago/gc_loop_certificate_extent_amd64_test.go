//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"errors"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
	"unsafe"
)

func TestGCLoopCertificateFirstDynamicExtent(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcLoopCertificatesModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err = in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	if in.c.genericGCFrameRoots() == nil || !in.gc.CollectsOnAllocation() {
		t.Fatal("fixture needs native checked reads")
	}
	ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
	view := in.gc.NativeView()
	handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
	base := view.Spaces[handle[gc.NativeHandleSpaceOffset]].Base
	off := binary.LittleEndian.Uint32(handle[gc.NativeHandleOffsetOffset:])
	header := unsafe.Slice((*byte)(offHeapPtr(base+uintptr(off))), int(gc.HeaderSize))
	saved := append([]byte(nil), header...)
	defer copy(header, saved)
	// The reference field ends at byte 20. The numeric field ends at byte 24.
	// Iteration zero skips the numeric field, so it must not require byte 24.
	binary.LittleEndian.PutUint32(header[4:], 20)
	got, err := in.Invoke("run", 1)
	if err != nil || len(got) != 1 || got[0] != 24 {
		t.Fatalf("smaller first access: %v %v", got, err)
	}
	_, err = in.Invoke("run", 2)
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
		t.Fatalf("larger dynamic access: %v, want extent trap", err)
	}
	// Type validation still happens on the first dynamic access.
	binary.LittleEndian.PutUint32(header[0:], ^uint32(0))
	_, err = in.Invoke("run", 1)
	if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
		t.Fatalf("first dynamic type check: %v", err)
	}
}
