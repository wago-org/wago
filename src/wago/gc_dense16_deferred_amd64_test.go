//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"errors"
	"testing"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCDense16HeapLoadKeepsValueAndTrapPoint(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0}, []byte{0x5f, 1, 0x6d, 1}, []byte{0x60, 0, 1, 0x7f})),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{2})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 1, 1, 0xd0, 1, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfb, 1, 0, 0xfb, 0, 1, 0x24, 0, 0x41, 1, 0x0b}),
			wasmtest.Code([]byte{0x41, 0, 0x23, 0, 0xfb, 2, 1, 0, 0xd1, 0x36, 2, 0, 0, 0x0b}),
		)),
	)
	code, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer code.Close()
	in, err := Instantiate(code, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	view := in.gc.NativeView()
	objectBytes := func(ref uint32) []byte {
		handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
		return unsafe.Slice((*byte)(offHeapPtr(view.Spaces[handle[gc.NativeHandleSpaceOffset]].Base+uintptr(binary.LittleEndian.Uint32(handle)))), int(binary.LittleEndian.Uint32(handle[gc.NativeHandleSizeOffset:])))
	}
	parent := objectBytes(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
	field := parent[gc.PayloadOffset:]
	child := binary.LittleEndian.Uint32(field)
	handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(child>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
	header := objectBytes(child)
	savedHandle := append([]byte(nil), handle...)
	savedType := binary.LittleEndian.Uint32(header)
	defer copy(handle, savedHandle)
	defer binary.LittleEndian.PutUint32(header, savedType)
	defer binary.LittleEndian.PutUint32(field, child)
	memory := unsafe.Slice((*byte)(offHeapPtr(in.jm.LinMemBase())), 4)
	for _, tc := range []struct {
		name    string
		prepare func()
		want    uint32
	}{
		{"null", func() { binary.LittleEndian.PutUint32(field, 0) }, 1},
		{"i31", func() { binary.LittleEndian.PutUint32(field, 3) }, 0},
		{"bad-handle", func() { binary.LittleEndian.PutUint32(field, 0xfffffffe) }, 0},
		{"bad-space", func() { handle[gc.NativeHandleSpaceOffset] = 255 }, 0},
		{"short-object", func() { binary.LittleEndian.PutUint32(handle[gc.NativeHandleSizeOffset:], gc.HeaderSize-1) }, 0},
		{"bad-type", func() { binary.LittleEndian.PutUint32(header, 0xffffffff) }, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy(handle, savedHandle)
			binary.LittleEndian.PutUint32(header, savedType)
			binary.LittleEndian.PutUint32(field, child)
			tc.prepare()
			binary.LittleEndian.PutUint32(memory, 0xdeadbeef)
			_, err := in.Invoke("run")
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapUnreachable {
				t.Fatalf("mint moved the trap: %v, want unreachable", err)
			}
			if got := binary.LittleEndian.Uint32(memory); got != tc.want {
				t.Fatalf("load changed value or skipped the store: %d, want%d", got, tc.want)
			}
		})
	}
}
