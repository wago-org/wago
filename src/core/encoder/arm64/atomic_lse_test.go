package arm64

import "testing"

// Golden words assembled independently by Clang with -march=armv8.1-a+lse.
func TestLSEAtomicEncodings(t *testing.T) {
	for i, size := range []int{1, 2, 4, 8} {
		for _, tc := range []struct {
			op   int
			word uint32
		}{
			{LSEAdd, 0x38e500e6}, {LSEClear, 0x38e510e6},
			{LSEXor, 0x38e520e6}, {LSESet, 0x38e530e6}, {LSESwap, 0x38e580e6},
		} {
			var a Asm
			a.AtomicRMWAL(tc.op, X5, X6, X7, size)
			if got, want := word(&a), tc.word|uint32(i)<<30; got != want {
				t.Fatalf("op=%d size=%d: %08x != %08x", tc.op, size, got, want)
			}
		}
		var a Asm
		a.CasAL(X5, X6, X7, size)
		if got, want := word(&a), uint32(0x08e5fce6)|uint32(i)<<30; got != want {
			t.Fatalf("cas size=%d: %08x != %08x", size, got, want)
		}
	}
}

func TestLSEAtomicRejectedOperands(t *testing.T) {
	for _, emit := range []func(*Asm){
		func(a *Asm) { a.AtomicRMWAL(LSEAdd, X5, XZR, X7, 8) },
		func(a *Asm) { a.AtomicRMWAL(LSEAdd, X5, 63, X7, 8) },
		func(a *Asm) { a.AtomicRMWAL(4, X5, X6, X7, 8) },
		func(a *Asm) { a.AtomicRMWAL(LSEAdd, X5, X6, X7, 3) },
		func(a *Asm) { a.CasAL(XZR, X6, X7, 8) },
		func(a *Asm) { a.CasAL(63, X6, X7, 8) },
		func(a *Asm) { a.CasAL(X5, X6, X7, 3) },
	} {
		func() {
			var a Asm
			defer func() {
				if recover() == nil || a.Len() != 0 {
					t.Fatal("invalid LSE instruction emitted bytes")
				}
			}()
			emit(&a)
		}()
	}
}
