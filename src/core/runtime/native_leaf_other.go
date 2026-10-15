//go:build (linux || darwin || windows) && (amd64 || arm64) && tinygo

package runtime

import "github.com/wago-org/wago/internal/runtimebridge"

// NativeScalarLeafRegisterEntryOffset is measured from the dispatch wrapper.
// The copied leaf is shorter than 128 bytes; 192 reserves the complete
// argument-buffer wrapper.
const NativeScalarLeafRegisterEntryOffset = 192

func RegisterNativeScalarLeaf(runtimebridge.HostScalarCallAccess, []byte, any, uint32) (bool, error) {
	return false, nil
}
func nativeScalarLeafPtr([]byte) uintptr { return 0 }
func unregisterNativeScalarLeaf([]byte)  {}

func BindNativeScalarLeafImport(runtimebridge.HostScalarCallAccess, []byte, []byte) bool {
	return false
}
