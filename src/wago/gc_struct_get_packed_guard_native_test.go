//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"runtime"
	"testing"
	"unsafe"
)

func TestGCPackedStructGetExtentFallback(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcPackedStructGetModule(0x77, true, true, true, true, 20))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{DisableCollection: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init", 0xffff); err != nil {
		t.Fatal(err)
	}
	view := in.gc.NativeView()
	saved := *view
	defer func() { *view = saved }()
	index := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)) >> 1
	handles := append([]byte(nil), unsafe.Slice((*byte)(offHeapPtr(view.Handles)), int(view.HandleCount)*gc.NativeHandleStride)...)
	handle := handles[int(index)*gc.NativeHandleStride:][:gc.NativeHandleStride]
	offset := gc.PayloadOffset + c.GCTypeDescs[0].Fields[20].Offset
	required := offset + 2
	object := make([]byte, required)
	binary.LittleEndian.PutUint16(object[offset:], 123)
	for _, mode := range []string{"handle extent", "header extent", "count"} {
		*view = saved
		view.Handles = uintptr(unsafe.Pointer(&handles[0]))
		space := &view.Spaces[handle[gc.NativeHandleSpaceOffset]]
		space.Base, space.Bytes = uintptr(unsafe.Pointer(&object[0])), required
		binary.LittleEndian.PutUint32(handle[gc.NativeHandleOffsetOffset:], 0)
		binary.LittleEndian.PutUint32(handle[gc.NativeHandleSizeOffset:], required)
		binary.LittleEndian.PutUint32(object[4:], required)
		switch mode {
		case "handle extent":
			binary.LittleEndian.PutUint32(handle[gc.NativeHandleSizeOffset:], required-1)
			binary.LittleEndian.PutUint32(object[4:], required-1)
		case "header extent":
			binary.LittleEndian.PutUint32(object[4:], required-1)
		case "count":
			view.HandleCount = index
		}
		got, err := in.Invoke("global")
		*view = saved
		if err != nil || len(got) != 1 || got[0] != 0xffffffff {
			t.Fatalf("%s: %v, %v; want signed helper value0xffffffff", mode, got, err)
		}
	}
	runtime.KeepAlive(handles)
	runtime.KeepAlive(object)
}

func TestGCPackedStructGetStaleReference(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcPackedStructGetModule(0x78, true, true, true, false, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init", 17); err != nil {
		t.Fatal(err)
	}
	stale := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
	if _, err := in.Invoke("init", 23); err != nil {
		t.Fatal(err)
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := in.gc.StructGetTyped(gc.Ref(uint32(stale)), in.requireGCDomainType(0), false, 0); err == nil {
		t.Fatal("fixture retained stale object")
	}
	saved := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
	writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, stale)
	_, err = in.Invoke("global")
	writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, saved)
	if err == nil {
		t.Fatal("stale struct reference accepted")
	}
}
