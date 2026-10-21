//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"runtime"
	"testing"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCGeneralScalarArrayGetExtentFallback(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("ARM64 native-view helper fallback contract")
	}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGeneralScalarArrayGetModule(0x7e, true, true, false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{DisableCollection: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init", 4, 17); err != nil {
		t.Fatal(err)
	}
	view := in.gc.NativeView()
	saved := *view
	var pinned runtime.Pinner
	defer pinned.Unpin()
	defer func() { *view = saved }()
	index := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)) >> 1
	handles := append([]byte(nil), unsafe.Slice((*byte)(offHeapPtr(view.Handles)), int(view.HandleCount)*gc.NativeHandleStride)...)
	handle := handles[int(index)*gc.NativeHandleStride:][:gc.NativeHandleStride]
	object := make([]byte, gc.PayloadOffset+32)
	pinned.Pin(&handles[0])
	pinned.Pin(&object[0])
	for _, mode := range []string{"element extent", "length", "large index"} {
		*view = saved
		view.Handles = uintptr(unsafe.Pointer(&handles[0]))
		space := &view.Spaces[handle[gc.NativeHandleSpaceOffset]]
		space.Base = uintptr(unsafe.Pointer(&object[0]))
		space.Bytes = uint32(len(object))
		binary.LittleEndian.PutUint32(handle[gc.NativeHandleOffsetOffset:], 0)
		binary.LittleEndian.PutUint32(handle[gc.NativeHandleSizeOffset:], uint32(len(object)))
		binary.LittleEndian.PutUint32(object[4:], uint32(len(object)))
		binary.LittleEndian.PutUint32(object[8:], 4)
		binary.LittleEndian.PutUint64(object[gc.PayloadOffset+8:], 999)
		readIndex := uint64(1)
		switch mode {
		case "element extent":
			binary.LittleEndian.PutUint32(object[4:], gc.PayloadOffset+8)
		case "length":
			binary.LittleEndian.PutUint32(object[8:], 1)
		case "large index":
			binary.LittleEndian.PutUint32(object[8:], 0xffffffff)
			readIndex = 0xfffffffe
		}
		out, err := in.Invoke("global", readIndex)
		*view = saved
		if mode == "large index" {
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapBuiltin {
				t.Fatalf("%s:%v,%v", mode, out, err)
			}
		} else if err != nil || len(out) != 1 || out[0] != 17 {
			t.Fatalf("%s:%v,%v", mode, out, err)
		}
	}
	runtime.KeepAlive(handles)
	runtime.KeepAlive(object)
}

func TestGCGeneralScalarArrayGetStale(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGeneralScalarArrayGetModule(0x7f, true, true, false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init", 4, 17); err != nil {
		t.Fatal(err)
	}
	stale := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
	if _, err := in.Invoke("init", 4, 23); err != nil {
		t.Fatal(err)
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := in.gc.ArrayGetTyped(gc.Ref(uint32(stale)), in.requireGCDomainType(0), false, 0); err == nil {
		t.Fatal("fixture kept stale object")
	}
	saved := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
	writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, stale)
	_, err = in.Invoke("global", 0)
	writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, saved)
	if err == nil {
		t.Fatal("stale array accepted")
	}
}
