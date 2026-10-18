//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"sync/atomic"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// GrowSharedMemory extends a stable reservation under the caller's memory lock.
// Commit before publishing the authoritative byte size, so other native threads
// can access every newly visible byte immediately. A zero delta ignores quota.
func (j *JobMemory) GrowSharedMemory(delta, policyMax uint32) uint32 {
	if j == nil {
		return ^uint32(0)
	}
	old := j.CurrentPages()
	if delta == 0 {
		return old
	}
	next := uint64(old) + uint64(delta)
	if next > uint64(j.MaxPages()) || policyMax != 0 && next > uint64(policyMax) {
		return ^uint32(0)
	}
	bytes := int(next << 16)
	if j.reserveBase != 0 {
		if err := growGuardedHostView(j, bytes); err != nil {
			return ^uint32(0)
		}
	}
	base := j.LinMemBase()
	atomic.StoreUint32((*uint32)(unsafe.Pointer(base-abi.LinMemCurrentPagesOffset)), uint32(next))
	atomic.StoreUint32((*uint32)(unsafe.Pointer(base-abi.LinMemLegacyBytesOffset)), uint32(bytes))
	atomic.StoreUint64((*uint64)(unsafe.Pointer(base-abi.ActualLinMemByteSize64Offset)), uint64(bytes))
	return old
}
