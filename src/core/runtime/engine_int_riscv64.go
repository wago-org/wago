//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/runtime/abi"
	goruntime "runtime"
	"unsafe"
)

//go:noescape
func enterNativeIntRV64Raw(code, linMem, trap, results, foreignStackTop, a0, a1, a2, a3 uintptr)

// The low 40 bytes are below the Engine's stack fence. Guest frames cannot
// reach them. Reuse this existing off-heap reservation for direct arguments
// and the result, without another mapping or any common Engine fields.
func (e *Engine) enterPreparedIntRV64(code, home uintptr, bounded bool, a0, a1, a2, a3 uint64) (uint64, error) {
	if e == nil || code == 0 || home == 0 || len(e.stack) < 40 {
		return 0, fmt.Errorf("jit: invalid RV64 prepared integer entry")
	}
	view := e.RV64InvocationViewPointer()
	*(*uint64)(unsafe.Pointer(home - abi.RV64InvocationViewOffset)) = uint64(view)
	args, results := e.stack[:32], e.stack[32:40]
	clear(results)
	e.prepareNativeInvocation(args, results)
	trap := uintptr(*(*uint64)(unsafe.Pointer(home - abi.TrapCellPtrOffset)))
	if trap == 0 {
		return 0, fmt.Errorf("jit: missing RV64 prepared trap cell")
	}
	if !bounded {
		nativeEnterSyscall()
	}
	enterNativeIntRV64Raw(code, home, trap, slicePtr(results), e.stackTop, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3))
	if !bounded {
		nativeExitSyscall()
	}
	value := binary.LittleEndian.Uint64(results)
	goruntime.KeepAlive(e)
	return value, nil
}
func (e *Engine) EnterPreparedInt(code, home uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return e.enterPreparedIntRV64(code, home, false, a0, a1, a2, a3)
}
func (e *Engine) EnterPreparedIntBounded(code, home uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return e.enterPreparedIntRV64(code, home, true, a0, a1, a2, a3)
}

// RV64 retains its complete save frame; no caller-clobber-only proof is claimed.
func (e *Engine) EnterPreparedIntLight(code, home uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return e.EnterPreparedInt(code, home, a0, a1, a2, a3)
}
func (e *Engine) EnterPreparedIntLightBounded(code, home uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	return e.EnterPreparedIntBounded(code, home, a0, a1, a2, a3)
}
func (e *Engine) PrepareIntCall(call *PreparedIntCall, code, home uintptr) {
	call.code, call.linMem, call.stack = code, home, e.stackTop
}
func (e *Engine) EnterPreparedIntPreboundContextBounded(call *PreparedIntCall, a0, a1, a2, a3 uint64) uint64 {
	value, _ := e.EnterPreparedIntBounded(call.code, call.linMem, a0, a1, a2, a3)
	return value
}
func PreparedIntTrapCode(trap []byte) TrapCode {
	if len(trap) < 4 {
		return TrapNone
	}
	return TrapCode(loadTrap(trap))
}
func ConsumePreparedIntTrap(trap []byte) error {
	tc := PreparedIntTrapCode(trap)
	if tc == TrapNone {
		return nil
	}
	storeTrap(trap, 0)
	return trapErrorFromBuffer(tc, trap)
}
