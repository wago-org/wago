//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"testing"
)

func TestIntegerQuotientRecurrenceMemoryAndState(t *testing.T) {
	for _, divisor := range []uint32{3, 5, 7, 19, 31, 127, 65537, 0x7fffffff, 0xfffffffb, 0xffffffff} {
		for _, count := range []uint32{16, 17, 18, 19, 32, 33, 64, 127} {
			for _, start := range []uint32{0, 1, 4, 31, 127} {
				limit := start + 1 + 2*count
				for _, extra := range []int{0, 70} {
					args := [4]uint64{uint64(start), uint64(start + 1), 0x88776655fffffff0, 0x9988776600000073}
					want, trap := integerByteMapOracle(args, divisor, 262, 63, limit)
					m := integerByteMapFixture(t, divisor, 262, 63, limit, extra, false)
					for _, enabled := range []bool{false, true} {
						got, err := runScalarUnrollMemory(t, m, CompileOptions{Optimizations: map[string]bool{"integer-quotient-recurrence": enabled}}, args[:]...)
						if (err != nil) != trap || !bytes.Equal(got, want) {
							t.Fatalf("divisor%x count%d start%d extra%d enabled%v err%v trap%v equal%v", divisor, count, start, extra, enabled, err, trap, bytes.Equal(got, want))
						}
					}
				}
			}
		}
	}
}

func TestIntegerQuotientRecurrenceSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, divisor := range []uint32{1, 3, 5, 7, 31, 127, 240, 241, 0x80000001, 0xfffffffb} {
		for _, enabled := range []bool{false, true} {
			stats := &ModuleStats{}
			cm, err := CompileModuleWith(integerByteMapFixture(t, divisor, 262, 64, 65, 0, false), CompileOptions{Stats: stats, Optimizations: map[string]bool{"integer-quotient-recurrence": enabled}})
			if err != nil {
				t.Fatal(err)
			}
			cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["integer-quotient-recurrence"]; (got != 0) != (enabled && divisor <= 240) {
				t.Fatalf("divisor%x enabled%v admissions%d", divisor, enabled, got)
			}
		}
	}
}

func TestIntegerQuotientRecurrenceByteBoundaries(t *testing.T) {
	for _, divisor := range []uint32{1, 2, 3, 4, 7, 15, 16, 17, 127, 128, 129, 239, 240, 241, 255, 256, 257} {
		for _, start := range []uint32{0, divisor - 1, divisor + 1} {
			for _, count := range []uint32{17, 18, 31, 32, 33, 65} {
				for _, coefficient := range []uint32{0, 131, 0x80000083, 0xffffffff} {
					limit := start + 1 + 2*count
					args := [4]uint64{uint64(start), uint64(start + 1), 0xfffffff0, uint64(uint32(0xfffffff0 + coefficient))}
					want, trap := integerByteMapOracle(args, divisor, 2*coefficient, 63, limit)
					m := integerByteMapFixture(t, divisor, 2*coefficient, 63, limit, 0, false)
					for _, enabled := range []bool{false, true} {
						got, err := runScalarUnrollMemory(t, m, CompileOptions{Optimizations: map[string]bool{"integer-quotient-recurrence": enabled}}, args[:]...)
						if (err != nil) != trap || !bytes.Equal(got, want) {
							t.Fatalf("divisor%d start%d count%d coefficient%x enabled%v err%v trap%v equal%v", divisor, start, count, coefficient, enabled, err, trap, bytes.Equal(got, want))
						}
					}
				}
			}
		}
	}
}
