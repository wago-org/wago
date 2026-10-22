//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"encoding/binary"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// NativeTailHostContinuation reads a parked proper-tail record under the native
// entry lease. The control, context and view come from the authenticated active
// instance, and the bounds belong to the physical entry stack. It never treats
// an arbitrary unmapped return address as permission to omit caller roots.
func NativeTailHostContinuation(ctrl, home, context, view, stackLimit, stackTop uintptr) (pc, frameTop uintptr, ok bool) {
	if ctrl < tailRACtrlOffset || stackTop < stackLimit {
		return 0, 0, false
	}
	record := ctrl - tailRACtrlOffset
	if record&15 != 0 || record < stackLimit || record > stackTop || stackTop-record < tailRAInvocationRecordBytes || view != record+tailRAPublicRecordBytes {
		return 0, 0, false
	}
	data := nativeImportStackSlice(record, tailRAInvocationRecordBytes)
	word := func(offset int) uintptr { return uintptr(binary.LittleEndian.Uint64(data[offset:])) }
	if binary.LittleEndian.Uint64(data[80:]) != tailRARecordMagic || word(104) != tailRAInvocationRecordBytes || word(64) != record+tailRAInvocationRecordBytes || word(96) != home || word(144) != context || binary.LittleEndian.Uint64(data[tailRAPublicRecordBytes:]) != abi.RV64InvocationMagic {
		return 0, 0, false
	}
	exit, err := nativeTailExitPointer()
	if err != nil || word(72) != exit || word(tailRAPublicRecordBytes+int(abi.RV64InvocationFirstRAOffset)) != exit {
		return 0, 0, false
	}
	return word(0), word(64), true
}
