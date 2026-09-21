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

func TestAMD64ScalarFPRRegisterOrderRetainsPrivateABI(t *testing.T) {
	for index, want := range []amd64.Reg{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12} {
		if got := amd64RailMachFPR(new(nativeBackendPlan), index); got != want {
			t.Fatalf("scalar FPR %d = XMM%d, want XMM%d", index, got, want)
		}
	}
	scalar := new(nativeBackendPlan)
	if got := amd64RailMachFPRResultScratch(scalar); got != 12 {
		t.Fatalf("scalar result scratch = XMM%d, want XMM12", got)
	}
	for ordinal, want := range []amd64.Reg{13, 14, 12} {
		if got := amd64RailMachFPROperandScratch(scalar, ordinal); got != want {
			t.Fatalf("scalar operand scratch %d = XMM%d, want XMM%d", ordinal, got, want)
		}
	}
	vector := &nativeBackendPlan{AMD64ShuffledFPRs: true}
	if got := amd64RailMachFPRResultScratch(vector); got != 13 {
		t.Fatalf("vector result scratch = XMM%d, want XMM13", got)
	}
	for ordinal, want := range []amd64.Reg{13, 14, 15} {
		if got := amd64RailMachFPROperandScratch(vector, ordinal); got != want {
			t.Fatalf("vector operand scratch %d = XMM%d, want XMM%d", ordinal, got, want)
		}
	}
}
