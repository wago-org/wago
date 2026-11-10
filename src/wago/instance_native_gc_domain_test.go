package wago

import (
	"encoding/binary"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func instanceNativeGCDomainID(in *Instance) uint64 {
	if in == nil || in.nativeContext == 0 {
		return 0
	}
	context := unsafe.Slice((*byte)(offHeapPtr(in.nativeContext)), coreruntime.InstanceContextBytes)
	return binary.LittleEndian.Uint64(context[coreruntime.InstanceContextGCDomainOffset:])
}
