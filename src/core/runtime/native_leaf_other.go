//go:build (linux || darwin || windows) && (amd64 || (arm64 && tinygo))

package runtime

import "github.com/wago-org/wago/internal/runtimebridge"

func RegisterNativeScalarLeaf(runtimebridge.HostScalarCallAccess, []byte, any, uint32) (bool, error) {
	return false, nil
}
func nativeScalarLeafPtr([]byte) uintptr { return 0 }
func unregisterNativeScalarLeaf([]byte)  {}

func BindNativeScalarLeafImport(runtimebridge.HostScalarCallAccess, []byte, []byte) bool {
	return false
}
