//go:build arm64

package wago

import (
	"fmt"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func compilerTierEntryThunks(count int) ([]byte, []uint32, error) {
	if uint64(count) > uint64(^uint32(0))/8 {
		return nil, nil, fmt.Errorf("tier entry count %d exceeds arm64 offset range", count)
	}
	a := new(a64.Asm)
	offsets := make([]uint32, count)
	for i := range offsets {
		offsets[i] = uint32(len(a.B))
		a.SubImm64(a64.X16, a64.X1, uint32(abi.TierEntriesPtrOffset))
		a.Load64(a64.X16, a64.X16, 0)
		a.MovImm64(a64.X17, uint64(i)*8)
		a.Add64(a64.X16, a64.X16, a64.X17)
		a.Load64(a64.X16, a64.X16, 0)
		a.Br(a64.X16)
	}
	return append([]byte(nil), a.B...), offsets, nil
}
