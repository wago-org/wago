//go:build (linux || darwin || windows) && (amd64 || arm64) && (windows || tinygo)

package runtime

import "unsafe"

func integerHostContextBridge(view bool) uintptr { return 0 }
func inlineHostIntegerContextEnter(fn FixedScalarHostContextCall, context unsafe.Pointer, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64 {
	panic("integer guest context unavailable")
}
func inlineHostIntegerViewContextEnter(fn FixedHostContextCallView, context unsafe.Pointer, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64 {
	panic("integer guest context unavailable")
}

func inlineHostIntegerI32Enter(fn func(int32) int32, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64 {
	panic("integer i32 context unavailable")
}
