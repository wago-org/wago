//go:build (linux || darwin) && arm64 && !tinygo

package runtime

import "unsafe"

func integerHostContextBridge(view bool) uintptr {
	if view {
		return inlineHostIntegerViewBridgeAddr()
	}
	return inlineHostIntegerBridgeAddr()
}
func inlineHostIntegerBridgeAddr() uintptr
func inlineHostIntegerViewBridgeAddr() uintptr

//go:noescape
func inlineHostIntegerContextEnter(fn FixedScalarHostContextCall, context unsafe.Pointer, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64

//go:noescape
func inlineHostIntegerViewContextEnter(fn FixedHostContextCallView, context unsafe.Pointer, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64

//go:noescape
func inlineHostIntegerI32Enter(fn func(int32) int32, code, args, linMem, trap, results, stack, ctrl uintptr, slots uint32) uint64
