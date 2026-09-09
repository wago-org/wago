//go:build (linux || darwin || windows) && arm64 && !tinygo

package runtime

//go:nosplit
func enterNativeIntLight(code, linMem, a0, a1, a2, a3, stack uintptr) uintptr {
	nativeEnterSyscall()
	result := enterNativeIntLightRaw(code, linMem, a0, a1, a2, a3, stack)
	nativeExitSyscall()
	return result
}
