//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"testing"
	"unsafe"

	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

func takenZeroFixture(wide, nonzero, inverse bool) (enc.Asm, int, int, int) {
	var a enc.Asm
	a.MovReg64(X1, X0)
	a.MovImm64(X0, 3)
	base := uint32(0x34000001)
	if wide {
		base |= 1 << 31
	}
	if nonzero {
		base |= 1 << 24
	}
	first := a.Len()
	a.B = append(a.B, 0, 0, 0, 0)
	a.AddImm32(X0, X0, 5)
	second := a.Len()
	a.B = append(a.B, 0, 0, 0, 0)
	a.AddImm32(X0, X0, 7)
	end := a.Len()
	a.Ret()
	wrWord(a.B, first, base|uint32((second-first)/4)<<5)
	if inverse {
		base ^= 1 << 24
	}
	wrWord(a.B, second, base|uint32((end-second)/4)<<5)
	return a, first, second, end
}

func TestTakenZeroBranchNativeExecution(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, nonzero := range []bool{false, true} {
			for _, inverse := range []bool{false, true} {
				a, first, second, end := takenZeroFixture(wide, nonzero, inverse)
				baseline := append([]byte(nil), a.B...)
				targets := []uint64{0}
				f := fn{}
				f.threadTakenZeroBranches(a.B, len(a.B), targets)
				wantTarget := end
				if inverse {
					wantTarget = second + 4
				}
				if target, _ := branchTarget(first, rdWord(a.B, first)); target != wantTarget || !branchTargeted(targets, wantTarget) {
					t.Fatalf("missing exact rewrite or new target: got%d want%d", target, wantTarget)
				}
				if !bytes.Equal(baseline[first+4:], a.B[first+4:]) {
					t.Fatal("alternative entrant changed")
				}
				for _, input := range []uint64{0, 1, 0xffffffff, 0xfeedface00000000, 1 << 32, ^uint64(0)} {
					v := input
					if !wide {
						v = uint64(uint32(v))
					}
					taken := (v != 0) == nonzero
					want := uint64(3)
					if !taken {
						want += 5
					}
					if taken == inverse {
						want += 7
					}
					for _, image := range [][]byte{baseline, a.B} {
						code, err := arm64spike.MapExec(image)
						if err != nil {
							t.Fatal(err)
						}
						got := arm64spike.Call2(uintptr(unsafe.Pointer(&code[0])), uintptr(input), 0)
						if uint64(got) != want {
							t.Fatalf("wide%v nonzero%v inverse%v input%x got%d want%d", wide, nonzero, inverse, input, got, want)
						}
					}
				}
			}
		}
	}
}

func TestTakenZeroBranchRejection(t *testing.T) {
	for _, shape := range []string{"different-width", "different-register", "outside-code", "self", "cycle", "opaque", "EH"} {
		a, first, second, _ := takenZeroFixture(false, false, false)
		f := fn{}
		switch shape {
		case "different-width":
			wrWord(a.B, second, rdWord(a.B, second)|(1<<31))
		case "different-register":
			wrWord(a.B, second, rdWord(a.B, second)^3)
		case "outside-code":
			wrWord(a.B, second, 0x340001e1)
		case "self":
			wrWord(a.B, first, 0x34000001)
		case "cycle":
			wrWord(a.B, second, 0x34000001|(uint32((first-second)/4)&0x7ffff)<<5)
		case "opaque":
			f.opaqueFragments = true
		case "EH":
			f.moduleEH = true
		}
		original := append([]byte(nil), a.B...)
		f.threadTakenZeroBranches(a.B, len(a.B), []uint64{0})
		if !bytes.Equal(original, a.B) {
			t.Fatalf("unsafe%s accepted", shape)
		}
	}
}

func TestTakenZeroBranchRangeLimit(t *testing.T) {
	// First target is at the furthest legal forward imm19 offset. The
	// second branch's target lies beyond that first branch's reach.
	b := make([]byte, (1<<20)+8)
	second := (1 << 20) - 4
	wrWord(b, 0, 0x34000001|uint32(second/4)<<5)
	wrWord(b, second, 0x34000041)
	wrWord(b, second+8, 0xd65f03c0)
	original := append([]byte(nil), b...)
	(&fn{}).threadTakenZeroBranches(b, len(b), make([]uint64, (len(b)/4+63)/64))
	if !bytes.Equal(original, b) {
		t.Fatal("out-of-range retarget accepted")
	}
}
