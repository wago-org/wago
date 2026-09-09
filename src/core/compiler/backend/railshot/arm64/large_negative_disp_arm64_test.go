//go:build arm64

package arm64

import (
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
	if got, want := len(f.a.B), 15*4; got != want {
		t.Fatalf("copy instance context emitted %d bytes, want %d", got, want)
	}
}
