//go:build !windows

package dragline

import (
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestAMD64FPRRegisterOrderReservesFixedVectorScratch(t *testing.T) {
	want := []amd64.Reg{0, 1, 2, 6, 7, 8, 9, 10, 11, 12, 3, 4, 5}
	if !slices.Equal(amd64FPRRegisters[:], want) {
		t.Fatalf("FPR register order = %v, want %v", amd64FPRRegisters, want)
	}
	for scratch := 1; scratch <= 3; scratch++ {
		allocatable := amd64FPRRegisters[:len(amd64FPRRegisters)-scratch]
		reserved := []amd64.Reg{5, 4, 3}[:scratch]
		for _, register := range reserved {
			if slices.Contains(allocatable, register) {
				t.Fatalf("scratch=%d leaves XMM%d allocatable in %v", scratch, register, allocatable)
			}
		}
	}
}
