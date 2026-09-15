//go:build amd64

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func compilerTierEntryThunks(count int) ([]byte, []uint32, error) {
	if uint64(count) > uint64(^uint32(0))/8 {
		return nil, nil, fmt.Errorf("tier entry count %d exceeds amd64 displacement range", count)
	}
	a := new(amd64.Asm)
	offsets := make([]uint32, count)
	for i := range offsets {
		offsets[i] = uint32(len(a.B))
		a.Load64(amd64.R11, amd64.RSI, -int32(abi.TierEntriesPtrOffset))
		a.Load64(amd64.R11, amd64.R11, int32(i*8))
		a.JmpReg(amd64.R11)
	}
	return append([]byte(nil), a.B...), offsets, nil
}
