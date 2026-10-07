//go:build amd64

package amd64

import (
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestSIMDBorrowExcludesImmutableCaches(t *testing.T) {
	f := fn{
		fconsts: []floatConstReg{{reg: 0}, {reg: 1}},
		vconsts: []v128ConstReg{{reg: 2}, {reg: 3}, {reg: 4}, {reg: 5}},
	}
	if got := f.borrowSIMDReg(maskOf(6, 7)); got != 8 {
		t.Fatalf("scratch = %d, want XMM8 beyond both cache banks and operands", got)
	}
	// Operand aliases do not consume another physical register.
	if got := f.borrowSIMDReg(maskOf(0, 0)); got != 6 {
		t.Fatalf("aliased scratch = %d, want XMM6", got)
	}
	// Borrowing preserves unrelated live values at the call site, so allocator
	// ownership and pinning must not make this bounded selection fail.
	f.fpinned = 0xffff
	if got := f.borrowSIMDReg(maskOf(6, 7)); got != 8 {
		t.Fatalf("pinned scratch = %d, want XMM8", got)
	}
}

func TestSIMDBorrowExhaustionRefuses(t *testing.T) {
	defer func() {
		if _, ok := recover().(regExhausted); !ok {
			t.Fatal("exhausted scratch selection did not refuse")
		}
	}()
	f := fn{}
	f.borrowSIMDReg(0xffff)
}

// Compile-only emission costs at the maximum production FP-cache count.
// Saved scratch contents and emitted code are never executed here.
func BenchmarkSIMDImmutableScratch(b *testing.B) {
	for _, kind := range []string{"lane", "binary", "gp-fallback"} {
		b.Run(kind, func(b *testing.B) {
			f := fn{a: &enc.Asm{B: make([]byte, 0, 4096)}, s: newStack(), sc: &scratch{},
				fconsts: []floatConstReg{{reg: 0}, {reg: 1}},
				vconsts: []v128ConstReg{{reg: 2}, {reg: 3}, {reg: 4}, {reg: 5}},
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				f.a.B = f.a.B[:0]
				switch kind {
				case "lane":
					f.extractSIMDLane(RAX, 7, 1, 8)
				case "binary":
					f.legacySIMDBinary(opVPaddd, 6, 7, 6)
				case "gp-fallback":
					f.simdFallback(0x00, 6, 7, 8)
				}
			}
			b.ReportMetric(float64(len(f.a.B)), "code-bytes")
		})
	}
}
