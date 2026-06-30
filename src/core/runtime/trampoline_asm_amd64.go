//go:build linux && amd64 && !tinygo

package runtime

// enterNative is implemented in trampoline_amd64.s for the standard Go
// toolchain. It switches RSP to the engine's foreign stack, calls the WARP
// WasmWrapper at code following the System V mapping (serArgs->RDI, linMem->RSI,
// trap->RDX, results->RCX), then restores the Go context. TinyGo cannot assemble
// Plan9 .s files, so it supplies its own enterNative in
// trampoline_tinygo_amd64.go.
func enterNative(code, serArgs, linMem, trap, results, foreignStackTop uintptr)
