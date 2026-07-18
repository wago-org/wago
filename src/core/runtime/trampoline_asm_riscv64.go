//go:build linux && riscv64 && !tinygo

package runtime

// enterNative is implemented in trampoline_riscv64.s. It switches to the
// engine's off-heap stack and enters the wrapper ABI with A0=serArgs,
// A1=linMem, A2=trap, A3=results. S9/X25 is the generated-code linMem register;
// Go CTXT/X26 and g/X27 are preserved and never exposed to the backend.
func enterNative(code, serArgs, linMem, trap, results, foreignStackTop uintptr)

// resumeNative restores a wasm activation parked by hostCallStub.
func resumeNative(ctrl, foreignStackTop uintptr)
