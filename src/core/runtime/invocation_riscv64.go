//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// RV64InvocationViewPointer reserves a64-byte view above all native entry and
// resume frames in this Engine's existing mmap. The caller retains the Engine
// and its actual argument/result buffers throughout entry, host park and resume.
// The view carries no Go roots. Private engines that do not bind a public
// invocation keep their existing entry stack layout.
func (e *Engine) RV64InvocationViewPointer() uintptr {
	e.stackTop = e.StackTop() - abi.RV64InvocationViewBytes
	return e.stackTop
}

func (e *Engine) prepareNativeInvocation(arguments, results []byte) {
	if e.stackTop != e.StackTop()-abi.RV64InvocationViewBytes {
		return
	}
	view := e.stack[len(e.stack)-abi.RV64InvocationViewBytes:]
	binary.LittleEndian.PutUint64(view, abi.RV64InvocationMagic)
	binary.LittleEndian.PutUint64(view[abi.RV64InvocationFenceOffset:], uint64(e.StackLimit()))
	binary.LittleEndian.PutUint64(view[abi.RV64InvocationTopOffset:], uint64(e.stackTop))
	binary.LittleEndian.PutUint64(view[abi.RV64InvocationArgumentsOffset:], uint64(slicePtr(arguments)))
	binary.LittleEndian.PutUint64(view[abi.RV64InvocationArgumentBytesOffset:], uint64(len(arguments)))
	binary.LittleEndian.PutUint64(view[abi.RV64InvocationResultsOffset:], uint64(slicePtr(results)))
	binary.LittleEndian.PutUint64(view[abi.RV64InvocationResultBytesOffset:], uint64(len(results)))
	binary.LittleEndian.PutUint64(view[56:], 0)
}

func (e *Engine) resetNativeInvocation() {
	clear(e.stack[len(e.stack)-abi.RV64InvocationViewBytes:])
	e.stackTop = e.StackTop()
}
