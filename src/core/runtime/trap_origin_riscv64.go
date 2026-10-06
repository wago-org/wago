//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func (j *JobMemory) SetRV64NativeContext(identity uintptr) {
	j.putU64(abi.RV64NativeContextOffset, uint64(identity))
}

func (j *JobMemory) RV64NativeContext() uintptr {
	return uintptr(j.getU64(abi.RV64NativeContextOffset))
}

func nativeTrapOrigin(trap []byte) (uintptr, bool) {
	return uintptr(binary.LittleEndian.Uint64(trap[8:16])), true
}
