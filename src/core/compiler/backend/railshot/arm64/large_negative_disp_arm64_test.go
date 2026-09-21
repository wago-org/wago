//go:build arm64

package arm64

import (
	"bytes"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestLoadLargeNegativeDisplacement(t *testing.T) {
	for _, size := range []int{4, 8} {
		f := &fn{a: &a64.Asm{}}
		if size == 8 {
			f.ld64(X0, X1, -288)
		} else {
			f.ld32(X0, X1, -288)
		}
		if got := len(f.a.B); got != 8 {
			t.Fatalf("%d-byte load emitted %d bytes, want address materialization plus load", size, got)
		}
	}
}

func TestCopyInstanceContextMaterializesLargeNegativeStore(t *testing.T) {
	f := &fn{a: &a64.Asm{}}
	f.copyInstanceContext(X1, X10)
	if got, want := len(f.a.B), 21*4; got != want {
		t.Fatalf("copy instance context emitted %d bytes, want %d", got, want)
	}
}

func TestAddDispMinimumSigned(t *testing.T) {
	for _, wide := range []bool{false, true} {
		f := &fn{a: &a64.Asm{}}
		f.addDisp(X0, X1, -0x80000000, wide)
		var want a64.Asm
		if wide {
			want.MovImm64(X16, 0xffffffff80000000)
			want.Add64(X0, X1, X16)
		} else {
			want.MovImm64(X16, 0x80000000)
			want.Add32(X0, X1, X16)
		}
		if !bytes.Equal(f.a.B, want.B) {
			t.Fatalf("wide=%t: code %x, want %x", wide, f.a.B, want.B)
		}
	}
}

func TestSPDisplacementUsesSPEncoding(t *testing.T) {
	for _, disp := range []int32{0, 16, 0x1000, 0x40f8, -0x40f8, -0x80000000} {
		for _, dst := range []Reg{X0, X16, X17} {
			for _, lea := range []bool{false, true} {
				f := &fn{a: &a64.Asm{}}
				if lea {
					f.leaDisp(dst, SP, disp, true)
				} else {
					f.addDisp(dst, SP, disp, true)
				}
				var want a64.Asm
				want.LeaSP(dst, disp)
				if !bytes.Equal(f.a.B, want.B) {
					t.Errorf("dst=%d disp=%#x lea=%t: code %x, want SP address %x", dst, disp, lea, f.a.B, want.B)
				}
			}
		}
	}
}
