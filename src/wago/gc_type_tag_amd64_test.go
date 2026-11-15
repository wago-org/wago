//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
	"unsafe"
)

func TestGCPackedTypeTagPublicationAndFullCast(t *testing.T) {
	module, _ := gcSpaceByteModule("defined-cast")
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(1), module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for range 16 {
		if _, err = in.Invoke("init"); err != nil {
			t.Fatal(err)
		}
	}
	ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
	view := in.gc.NativeView()
	h := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
	metadata := binary.LittleEndian.Uint32(h[gc.NativeHandleMetadataOffset:])
	tag := metadata>>5&127 | metadata>>12&0xf80
	sp := h[gc.NativeHandleSpaceOffset] & gc.NativeHandleSpaceMask
	object := unsafe.Slice((*byte)(offHeapPtr(view.Spaces[sp].Base+uintptr(binary.LittleEndian.Uint32(h)))), int(binary.LittleEndian.Uint32(h[gc.NativeHandleSizeOffset:])))
	typ := binary.LittleEndian.Uint32(object)
	if typ >= 4095 || tag != typ+1 {
		t.Fatalf("native publication type%d tag%d; want canonicalID+1", typ, tag)
	}
	good := func() {
		t.Helper()
		got, e := in.Invoke("run", 3)
		if e != nil || len(got) != 1 || got[0] != 3 {
			t.Fatalf("cast=%v,%v", got, e)
		}
	}
	bad := func() {
		t.Helper()
		_, e := in.Invoke("run", 1)
		var trap *TrapError
		if !errors.As(e, &trap) || trap.Code != TrapCastFailure || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 0 || trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != ^uint32(0) {
			t.Fatalf("cast=%v; want function0 cast failure", e)
		}
	}
	func() {
		good()
		// Test-only header poisoning isolates cache use. It is not a supported public
		// mutation or a promised corruption detector. Restore before GC and teardown.
		binary.LittleEndian.PutUint32(object, ^uint32(0))
		defer binary.LittleEndian.PutUint32(object, typ)
		good()
		binary.LittleEndian.PutUint32(h[gc.NativeHandleMetadataOffset:], metadata&^0x00f80fe0)
		bad() // unknown tag must use the original header path
		binary.LittleEndian.PutUint32(object, typ)
		good()
		binary.LittleEndian.PutUint32(h[gc.NativeHandleMetadataOffset:], metadata)
		saved := append([]byte(nil), h...)
		defer copy(h, saved)
		for _, v := range []struct {
			offset int
			value  uint32
		}{{gc.NativeHandleOffsetOffset, ^uint32(0)}, {gc.NativeHandleSizeOffset, gc.HeaderSize - 1}, {gc.NativeHandleSizeOffset, ^uint32(0)}} {
			binary.LittleEndian.PutUint32(h[v.offset:], v.value)
			bad()
			copy(h, saved)
		}
		h[gc.NativeHandleSpaceOffset] = (h[gc.NativeHandleSpaceOffset] &^ gc.NativeHandleSpaceMask) | 5
		bad()
		copy(h, saved)
	}()
	if err = in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	good()
	view = in.gc.NativeView()
	moved := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
	movedMetadata := binary.LittleEndian.Uint32(moved[gc.NativeHandleMetadataOffset:])
	if got := movedMetadata>>5&127 | movedMetadata>>12&0xf80; got != tag {
		t.Fatalf("collection changed canonical tag%d to%d", tag, got)
	}
	if err = in.Close(); err != nil {
		t.Fatal(err)
	}
	if err = in.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !in.resourcesClosed || in.hasPhysicalResources() {
		t.Fatal("physical resources retained after completion")
	}
}
