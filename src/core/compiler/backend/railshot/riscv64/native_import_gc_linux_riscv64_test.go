//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"encoding/binary"
	"fmt"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestNativeImportHostContinuationChecksActivation(t *testing.T) {
	for _, transportBytes := range []int{nativeImportTransportBytes, nativePublicImportTransportBytes} {
		t.Run(fmt.Sprintf("bytes%d", transportBytes), func(t *testing.T) { testNativeImportHostContinuation(t, transportBytes) })
	}
}

func testNativeImportHostContinuation(t *testing.T, transportBytes int) {
	const controlOffset = nativeImportFrameBytes + 16
	viewOffset := controlOffset + HostFixedFrameBytes + 2*transportBytes
	frameBytes := viewOffset + abi.RV64InvocationViewBytes
	arena, err := coreruntime.NewArena(frameBytes + 160)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	data := arena.Alloc(frameBytes + 160)
	record := tailRAPointer(data)
	top := record + uintptr(len(data))
	const context, callerContext, home, pc = uintptr(0x10000), uintptr(0x20000), uintptr(0x30000), uintptr(0x40000)
	put := func(off int, value uintptr) { binary.LittleEndian.PutUint64(data[off:], uint64(value)) }
	put(0, pc)
	put(8, home)
	put(16, callerContext)
	put(nativeImportFrameBytes, context)
	put(72, top-abi.RV64InvocationViewBytes)
	put(viewOffset, uintptr(abi.RV64InvocationMagic))
	put(viewOffset+int(abi.RV64InvocationFenceOffset), record)
	put(viewOffset+int(abi.RV64InvocationTopOffset), top-abi.RV64InvocationViewBytes)
	put(viewOffset+int(abi.RV64InvocationArgumentsOffset), record+controlOffset+HostFixedFrameBytes)
	put(viewOffset+int(abi.RV64InvocationResultsOffset), record+controlOffset+HostFixedFrameBytes+uintptr(transportBytes))
	put(viewOffset+int(abi.RV64InvocationArgumentBytesOffset), uintptr(transportBytes))
	put(viewOffset+int(abi.RV64InvocationResultBytesOffset), uintptr(transportBytes))
	read := func() bool {
		gotPC, gotHome, gotContext, base, parent, ok := NativeImportHostContinuation(record+controlOffset, context, record+uintptr(viewOffset), record, top)
		return ok && gotPC == pc && gotHome == home && gotContext == callerContext && base == record+uintptr(frameBytes) && parent == top-abi.RV64InvocationViewBytes
	}
	if !read() {
		t.Fatal("valid native import activation rejected")
	}
	for _, off := range []int{nativeImportFrameBytes, 72, viewOffset, viewOffset + int(abi.RV64InvocationFenceOffset), viewOffset + int(abi.RV64InvocationTopOffset), viewOffset + int(abi.RV64InvocationArgumentsOffset), viewOffset + int(abi.RV64InvocationResultsOffset), viewOffset + int(abi.RV64InvocationArgumentBytesOffset), viewOffset + int(abi.RV64InvocationResultBytesOffset)} {
		old := binary.LittleEndian.Uint64(data[off:])
		put(off, uintptr(old)^8)
		if read() {
			t.Fatalf("damaged word %d accepted", off)
		}
		put(off, uintptr(old))
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if !read() {
			panic("native import activation")
		}
	}); allocations != 0 {
		t.Fatalf("continuation allocations=%g", allocations)
	}
}
