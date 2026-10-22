//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"encoding/binary"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// NativeImportHostContinuation reads only an authenticated stack-owned native
// import activation. Its control and view belong to the active target under the
// native entry lease; all pointers must fit the physical invocation stack.
func NativeImportHostContinuation(ctrl, context, view, stackLimit, stackTop uintptr) (pc, home, callerContext, callerBase, parentView uintptr, ok bool) {
	for _, transportBytes := range [...]int{nativePublicImportTransportBytes, nativeImportTransportBytes} {
		if pc, home, callerContext, callerBase, parentView, ok = nativeImportHostContinuationBytes(ctrl, context, view, stackLimit, stackTop, transportBytes); ok {
			return
		}
	}
	return
}

func nativeImportHostContinuationBytes(ctrl, context, view, stackLimit, stackTop uintptr, transportBytes int) (pc, home, callerContext, callerBase, parentView uintptr, ok bool) {
	const controlOffset = nativeImportFrameBytes + 16
	viewOffset := controlOffset + HostFixedFrameBytes + 2*transportBytes
	frameBytes := viewOffset + abi.RV64InvocationViewBytes
	if ctrl < controlOffset || stackTop < stackLimit {
		return
	}
	record := ctrl - controlOffset
	if record&15 != 0 || record < stackLimit || record > stackTop || stackTop-record < uintptr(frameBytes) || view != record+uintptr(viewOffset) {
		return
	}
	data := nativeImportStackSlice(record, frameBytes)
	word := func(offset int) uintptr { return uintptr(binary.LittleEndian.Uint64(data[offset:])) }
	if word(nativeImportFrameBytes) != context || binary.LittleEndian.Uint64(data[viewOffset:]) != abi.RV64InvocationMagic || word(viewOffset+int(abi.RV64InvocationFenceOffset)) != stackLimit || word(viewOffset+int(abi.RV64InvocationTopOffset)) != stackTop-abi.RV64InvocationViewBytes || word(viewOffset+int(abi.RV64InvocationArgumentsOffset)) != record+controlOffset+HostFixedFrameBytes || word(viewOffset+int(abi.RV64InvocationResultsOffset)) != record+controlOffset+HostFixedFrameBytes+uintptr(transportBytes) || word(viewOffset+int(abi.RV64InvocationArgumentBytesOffset)) != uintptr(transportBytes) || word(viewOffset+int(abi.RV64InvocationResultBytesOffset)) != uintptr(transportBytes) {
		return
	}
	parentView = word(72)
	if parentView&7 != 0 || parentView < record+uintptr(frameBytes) || parentView > stackTop || stackTop-parentView < abi.RV64InvocationViewBytes {
		return 0, 0, 0, 0, 0, false
	}
	return word(0), word(8), word(16), record + uintptr(frameBytes), parentView, true
}

// NativeImportParentHostControl derives the control location of a previously
// authenticated non-root parent view. The continuation reader must authenticate
// its complete record and retained target context before using this address.
func NativeImportParentHostControl(view uintptr) uintptr {
	const between = HostFixedFrameBytes + 2*nativeImportTransportBytes
	if view < between {
		return 0
	}
	return view - between
}

// NativeImportParentHostControlInStack derives a candidate parent control only
// from a complete retained view range. Its caller must still authenticate the
// resulting activation and owner before it uses any saved return address.
func NativeImportParentHostControlInStack(view, stackLimit, stackTop uintptr) uintptr {
	if view&7 != 0 || view < stackLimit || view > stackTop || stackTop-view < abi.RV64InvocationViewBytes {
		return 0
	}
	data := nativeImportStackSlice(view, abi.RV64InvocationViewBytes)
	arguments := binary.LittleEndian.Uint64(data[abi.RV64InvocationArgumentBytesOffset:])
	results := binary.LittleEndian.Uint64(data[abi.RV64InvocationResultBytesOffset:])
	if arguments != results || arguments != nativeImportTransportBytes && arguments != nativePublicImportTransportBytes {
		return 0
	}
	between := uintptr(HostFixedFrameBytes) + 2*uintptr(arguments)
	if view < between || view-between < stackLimit {
		return 0
	}
	return view - between
}

// The caller has checked this complete record against the retained native stack.
// Avoid a Go-heap temporary when reading its numeric address under checkptr=2.
//
//go:nocheckptr
func nativeImportStackSlice(address uintptr, length int) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(address)), length)
}
