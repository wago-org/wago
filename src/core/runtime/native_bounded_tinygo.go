//go:build (linux || darwin || windows) && (amd64 || arm64) && tinygo

package runtime

func enterNativeBounded(code, serArgs, linMem, trap, results, stack uintptr) {
	enterNative(code, serArgs, linMem, trap, results, stack)
}
