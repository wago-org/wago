//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Run the leaf independently of the public reference boundary so malformed
// compact operands reach the actual native guards.
func TestGCObjectStubPhysicalGuards(t *testing.T) {
	var a a64.Asm
	a.StpPre(linMemReg, LR, SP, -16)
	a.MovReg64(linMemReg, X1)
	a.MovReg64(X16, X0)
	a.MovImm32(X11, int32(gc.HeaderSize))
	call := a.Bl()
	a.MovReg64(X0, X17)
	// The required extent is part of the fixed preserved-register contract.
	a.CmpImm32(X11, gc.HeaderSize)
	good := a.Bcond(condE)
	a.MovImm32(X0, 2)
	if !a.PatchBranch19(good, a.Len()) {
		t.Fatal("patch extent check")
	}
	a.LdpPost(linMemReg, LR, SP, 16)
	a.Ret()
	stub := a.Len()
	a.B = emitGCObjectStub(a.B)
	if !a.PatchBranch26(call, stub) {
		t.Fatal("patch checked-object call")
	}
	code, entry, err := rt.MapCode(a.B)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unmap(code)
	var handles [2 * gc.NativeHandleStride]byte
	var header [gc.HeaderSize]byte
	var basedata [abi.GCNativeViewPtrOffset + 8]byte
	collector := gc.NativeCollectorView{}
	var instance gc.NativeInstanceView
	// t.Run can grow the parent Go stack. Published uintptr fields must point
	// to stable objects, just like the collector views used by real instances.
	var pinned runtime.Pinner
	for _, object := range []any{&handles, &header, &basedata, &collector, &instance} {
		pinned.Pin(object)
	}
	defer pinned.Unpin()
	instance.Collector = uintptr(unsafe.Pointer(&collector))
	binary.LittleEndian.PutUint64(basedata[:], uint64(uintptr(unsafe.Pointer(&instance))))
	handle := handles[gc.NativeHandleStride:]
	for _, name := range []string{"nursery", "old", "large", "tiny", "null", "i31", "high bits", "count", "free", "unknown space", "offset", "size", "short handle", "short header", "long header", "space bytes"} {
		t.Run(name, func(t *testing.T) {
			collector = gc.NativeCollectorView{Handles: uintptr(unsafe.Pointer(&handles[0])), HandleCount: 2}
			clear(handle)
			binary.LittleEndian.PutUint32(handle[gc.NativeHandleSizeOffset:], gc.HeaderSize)
			binary.LittleEndian.PutUint32(header[4:], gc.HeaderSize)
			space := byte(gc.NativeSpaceNursery)
			ref := uint64(2)
			want := uintptr(1)
			switch name {
			case "nursery":
				want = 0
			case "old":
				space, want = gc.NativeSpaceOld, 0
			case "large":
				space, want = gc.NativeSpaceLarge, 0
			case "tiny":
				space, want = gc.NativeSpaceTiny, 0
			case "null":
				ref = 0
			case "i31":
				ref = 3
			case "high bits":
				ref |= uint64(1) << 40
			case "count":
				collector.HandleCount = 1
			case "free":
				space = gc.NativeSpaceFree
			case "unknown space":
				space = 255
			case "offset":
				binary.LittleEndian.PutUint32(handle[gc.NativeHandleOffsetOffset:], ^uint32(0))
			case "size":
				binary.LittleEndian.PutUint32(handle[gc.NativeHandleSizeOffset:], ^uint32(0))
			case "short handle":
				binary.LittleEndian.PutUint32(handle[gc.NativeHandleSizeOffset:], gc.HeaderSize-1)
			case "short header":
				binary.LittleEndian.PutUint32(header[4:], gc.HeaderSize-1)
			case "long header":
				binary.LittleEndian.PutUint32(header[4:], gc.HeaderSize+1)
			}
			handle[gc.NativeHandleSpaceOffset] = space
			if space < gc.NativeSpaceCount {
				collector.Spaces[space] = gc.NativeSpaceView{Base: uintptr(unsafe.Pointer(&header[0])), Bytes: gc.HeaderSize}
				if name == "space bytes" {
					collector.Spaces[space].Bytes = 0
				}
			}
			got := arm64spike.Call2(entry, uintptr(ref), uintptr(unsafe.Pointer(&basedata[abi.GCNativeViewPtrOffset])))
			if got != want {
				t.Fatalf("status=%d, want%d", got, want)
			}
		})
	}
	runtime.KeepAlive(&handles)
	runtime.KeepAlive(&header)
	runtime.KeepAlive(&collector)
	runtime.KeepAlive(&instance)
	runtime.KeepAlive(&basedata)
}
