package gc

import "github.com/wago-org/wago/src/core/nativeabi"

// NativeRootKind and related aliases expose the native frame contract at the
// collector boundary without making compiler backends depend on collector
// implementation details.
type NativeRootKind = nativeabi.RootKind
type NativeRootSlot = nativeabi.RootSlot
type NativeRootMap = nativeabi.FunctionRootMap

const (
	NativeRootGCRef   = nativeabi.RootGCRef
	NativeRootFuncRef = nativeabi.RootFuncRef
)

// ValidateNativeRootMaps rejects malformed frame metadata before a collector or
// reference-lifecycle scanner can trust native offsets.
func ValidateNativeRootMaps(maps []NativeRootMap, localFunctions int) error {
	return nativeabi.ValidateRootMaps(maps, localFunctions)
}
