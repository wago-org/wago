//go:build windows && amd64 && !tinygo

package runtime

import "unsafe"

// The prepared numeric owner uses these static ABIInternal entries. The
// generic bounded callers retain their ordinary Windows fallback.
//go:noescape
func inlineHostContextEnter(fn FixedScalarHostContextCall, context unsafe.Pointer, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64

//go:noescape
func inlineHostViewContextEnter(fn FixedHostContextCallView, context unsafe.Pointer, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64
