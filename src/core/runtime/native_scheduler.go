//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package runtime

import _ "unsafe"

// The runtime records a scannable Go-stack boundary before foreign entry.
// No Go allocation or stack growth is allowed until exitsyscall reacquires a P.
// Call sites retain slice owners across native execution. Raw uintptr inputs
// require an external owner and stable storage. Host calls return through
// exitsyscall before dispatching Go callbacks.
//
//go:linkname nativeEnterSyscall runtime.entersyscall
func nativeEnterSyscall()

//go:linkname nativeExitSyscall runtime.exitsyscall
func nativeExitSyscall()

//go:nosplit
func enterNative(code, serArgs, linMem, trap, results, stack uintptr) {
	nativeEnterSyscall()
	enterNativeRaw(code, serArgs, linMem, trap, results, stack)
	nativeExitSyscall()
}

//go:nosplit
func resumeNative(ctrl, stack uintptr) {
	nativeEnterSyscall()
	resumeNativeRaw(ctrl, stack)
	nativeExitSyscall()
}

//go:nosplit
func enterNativeInt(code, linMem, a0, a1, a2, a3, stack uintptr) uintptr {
	nativeEnterSyscall()
	result := enterNativeIntRaw(code, linMem, a0, a1, a2, a3, stack)
	nativeExitSyscall()
	return result
}
