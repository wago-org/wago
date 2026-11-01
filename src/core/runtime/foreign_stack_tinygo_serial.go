//go:build tinygo && (scheduler.tasks || scheduler.none) && ((linux && amd64) || ((linux || darwin) && arm64))

package runtime

import "unsafe"

// Serial schedulers cannot collect while native code runs: it neither allocates
// Go objects nor calls Go on the foreign stack. Host calls first return through
// the trampoline, restoring the host stack and its callee-saved Go registers.
func registerTinyGoForeignStack(low, high, hostSP uintptr) unsafe.Pointer { return nil }
func unregisterTinyGoForeignStack(token unsafe.Pointer)                   {}
