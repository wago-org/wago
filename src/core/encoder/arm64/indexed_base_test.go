package arm64

import (
	"fmt"
	"testing"
)

// Exercise the public emitter as well as the instruction classifier: every
// integer load width must invalidate all three address registers, but retain
// the three-instruction sequence when its destination is independent.
func TestIndexedBaseIntegerLoads(t *testing.T) {
	for _, load := range []struct {
		name         string
		size         int
		signed, wide bool
	}{
		{"ldrb", 1, false, false}, {"ldrh", 2, false, false},
		{"ldr-w", 4, false, false}, {"ldr-x", 8, false, true},
		{"ldrsb-w", 1, true, false}, {"ldrsh-w", 2, true, false},
		{"ldrsb-x", 1, true, true}, {"ldrsh-x", 2, true, true},
		{"ldrsw", 4, true, true},
	} {
		for _, dst := range []Reg{X26, X22, X16, X0, XZR} {
			for _, stable := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/rt%d/stable=%t", load.name, dst, stable), func(t *testing.T) {
					a := Asm{DenseIdxDisp: true, ReuseIndexedBase: true}
					a.LoadIdx(dst, X26, X22, 8, load.size, load.signed, load.wide)
					if stable {
						a.Add32(X3, X3, X4)
					}
					checkIndexedBaseReuse(t, &a, dst == X0 || dst == XZR)
				})
			}
		}
	}
}

// Repeating a canonicalization is only harmless while the earlier 32-bit
// value is still intact. A signed load between the two MOVs breaks that proof.
func TestIndexedBaseCanonicalSignedLoad(t *testing.T) {
	for _, dst := range []Reg{X26, X22, X16, X0} {
		t.Run(fmt.Sprintf("rt%d", dst), func(t *testing.T) {
			a := Asm{DenseIdxDisp: true, ReuseIndexedBase: true}
			a.MovReg32(X22, X22)
			a.LoadIdx(dst, X26, X22, 4, 1, true, true)
			a.MovReg32(X22, X22)
			checkIndexedBaseReuse(t, &a, dst == X0)
		})
	}
}

func TestIndexedBaseEqualInputs(t *testing.T) {
	for _, dst := range []Reg{X22, X16, X0} {
		t.Run(fmt.Sprintf("rt%d", dst), func(t *testing.T) {
			a := Asm{DenseIdxDisp: true, ReuseIndexedBase: true}
			a.LoadIdx(dst, X22, X22, 4, 1, true, true)
			a.LoadIdx(X1, X22, X22, 8, 4, false, false)
			wantBytes, wantReuses := 16, 0
			if dst == X0 {
				wantBytes, wantReuses = 12, 1
			}
			if len(a.B) != wantBytes || a.IndexedBaseReuses != wantReuses {
				t.Fatalf("%d bytes/%d reuses, want %d/%d", len(a.B), a.IndexedBaseReuses, wantBytes, wantReuses)
			}
		})
	}
}

// Opcode fixtures cross-checked with clang 19 targeting aarch64-linux-gnu.
// These architectural opcode fixtures include V and both opc bits. In
// particular, STR Q has opc=10 while the similarly numbered Vt and Xt never
// alias. The register-offset variant is the unscaled Xm form emitted by Wago.
func TestIndexedBaseMemoryClasses(t *testing.T) {
	for _, mem := range []struct {
		name     string
		op       uint32
		writesRt bool
	}{
		{"strb", 0x39000000, false}, {"strh", 0x79000000, false},
		{"str-w", 0xb9000000, false}, {"str-x", 0xf9000000, false},
		{"ldrb", 0x39400000, true}, {"ldrh", 0x79400000, true},
		{"ldr-w", 0xb9400000, true}, {"ldr-x", 0xf9400000, true},
		{"ldrsb-w", 0x39c00000, true}, {"ldrsh-w", 0x79c00000, true},
		{"ldrsb-x", 0x39800000, true}, {"ldrsh-x", 0x79800000, true},
		{"ldrsw", 0xb9800000, true},
		{"str-b", 0x3d000000, false}, {"str-h", 0x7d000000, false},
		{"str-s", 0xbd000000, false}, {"str-d", 0xfd000000, false},
		{"str-q", 0x3d800000, false},
		{"ldr-b", 0x3d400000, false}, {"ldr-h", 0x7d400000, false},
		{"ldr-s", 0xbd400000, false}, {"ldr-d", 0xfd400000, false},
		{"ldr-q", 0x3dc00000, false},
		// Prefetch does not write a GPR; the classifier intentionally
		// overestimates its clobbers rather than special-casing unused PRFM.
		{"prfm-conservative", 0xf9800000, true},
	} {
		for _, rt := range []Reg{X26, X22, X16, X0, XZR} {
			for _, form := range []string{"adjacent", "immediate-window", "register-window"} {
				t.Run(fmt.Sprintf("%s/rt%d/%s", mem.name, rt, form), func(t *testing.T) {
					a := Asm{DenseIdxDisp: true, ReuseIndexedBase: true}
					a.AddShifted(X16, X26, X22, 0, false)
					op := mem.op | r(X16)<<5 | r(rt)
					if form != "adjacent" {
						a.Store32(X1, X16, 0)
						// Intervening loads from unrelated addresses still
						// invalidate the cached base if they overwrite it.
						op = mem.op | r(X9)<<5 | r(rt)
					}
					if form == "register-window" {
						op = op&^0x01000000 | 0x00206800 | r(X6)<<16
					}
					a.word(op)
					checkIndexedBaseReuse(t, &a, !mem.writesRt || rt == X0 || rt == XZR)
				})
			}
		}
	}
}

// Test effects, including stores which write a status register or their base,
// and loads with a second destination. Unknown families must end the proof.
func TestIndexedBaseInterveningInstructions(t *testing.T) {
	for _, ins := range []struct {
		name     string
		emit     func(*Asm, Reg)
		knownALU bool
	}{
		{"add-w", func(a *Asm, dst Reg) { a.Add32(dst, X3, X4) }, true},
		{"sub-x", func(a *Asm, dst Reg) { a.Sub64(dst, X3, X4) }, true},
		{"adds-x", func(a *Asm, dst Reg) { a.Adds64(dst, X3, X4) }, true},
		{"subs-x", func(a *Asm, dst Reg) { a.Subs64(dst, X3, X4) }, true},
		{"add-imm", func(a *Asm, dst Reg) { a.AddImm64(dst, X3, 4) }, true},
		{"sub-imm", func(a *Asm, dst Reg) { a.SubImm32(dst, X3, 4) }, true},
		{"add-extended", func(a *Asm, dst Reg) { a.AddExtUXTW(dst, X3, X4) }, true},
		{"mov-w", func(a *Asm, dst Reg) { a.MovReg32(dst, X3) }, false},
		{"mov-x", func(a *Asm, dst Reg) { a.MovReg64(dst, X3) }, false},
		{"movz", func(a *Asm, dst Reg) { a.MovImm64(dst, 7) }, false},
		{"fmov-to-gpr", func(a *Asm, dst Reg) { a.FmovToGpr(dst, X3, true) }, false},
		{"ldur", func(a *Asm, dst Reg) { a.Ldur64(dst, X9, -8) }, false},
		{"ldp-first", func(a *Asm, dst Reg) { a.LdpOffset(dst, X2, X9, 0) }, false},
		{"ldp-second", func(a *Asm, dst Reg) { a.LdpOffset(X2, dst, X9, 0) }, false},
		{"ldp-post-base", func(a *Asm, dst Reg) { a.LdpPost(X1, X2, dst, 16) }, false},
		{"stp-pre-base", func(a *Asm, dst Reg) { a.StpPre(X1, X2, dst, -16) }, false},
		{"ldr-pre-base", func(a *Asm, dst Reg) { a.word(0xf8408c01 | r(dst)<<5) }, false},
		{"str-post-base", func(a *Asm, dst Reg) { a.word(0xf8008401 | r(dst)<<5) }, false},
		{"ldr-q-post-base", func(a *Asm, dst Reg) { a.word(0x3cc10401 | r(dst)<<5) }, false},
		{"ldr-scaled-register", func(a *Asm, dst Reg) { a.word(0xf8667920 | r(dst)) }, false},
		{"ldr-extended-register", func(a *Asm, dst Reg) { a.word(0xf8664920 | r(dst)) }, false},
		{"ld1-post-base", func(a *Asm, dst Reg) { a.word(0x4cdf7000 | r(dst)<<5) }, false},
		{"st1-post-base", func(a *Asm, dst Reg) { a.word(0x4c9f7000 | r(dst)<<5) }, false},
		{"ldr-literal", func(a *Asm, dst Reg) { a.word(0x58000000 | r(dst)) }, false},
		{"ldar", func(a *Asm, dst Reg) { a.Ldar(dst, X9, 8) }, false},
		{"ldaxr", func(a *Asm, dst Reg) { a.Ldaxr(dst, X9, 8) }, false},
		{"stlxr-status", func(a *Asm, dst Reg) { a.Stlxr(dst, X1, X9, 8) }, false},
		{"branch", func(a *Asm, _ Reg) { a.word(0x14000000) }, false},
		{"call", func(a *Asm, _ Reg) { a.Bl() }, false},
		{"unknown", func(a *Asm, _ Reg) { a.word(0) }, false},
	} {
		for _, dst := range []Reg{X26, X22, X16, X0} {
			t.Run(fmt.Sprintf("%s/dst%d", ins.name, dst), func(t *testing.T) {
				a := Asm{DenseIdxDisp: true, ReuseIndexedBase: true}
				a.LoadIdx(X1, X26, X22, 4, 4, false, false)
				ins.emit(&a, dst)
				checkIndexedBaseReuse(t, &a, ins.knownALU && dst == X0)
			})
		}
	}
}

// Both consumers must reconstruct the address with the exact ADD when the
// proof fails. Checking emitted instructions prevents a counter-only fix from
// satisfying the regression, and safe cases must keep their instruction count.
func checkIndexedBaseReuse(t *testing.T, prefix *Asm, safe bool) {
	t.Helper()
	for _, store := range []bool{false, true} {
		a := *prefix
		a.B = append([]byte(nil), prefix.B...)
		before := len(a.B)
		if store {
			a.StoreIdx(X26, X22, X2, 8, 4)
		} else {
			a.LoadIdx(X2, X26, X22, 8, 4, false, false)
		}
		wantBytes, wantReuses := before+8, 0
		if safe {
			wantBytes, wantReuses = before+4, 1
		}
		if len(a.B) != wantBytes || a.IndexedBaseReuses != wantReuses {
			t.Fatalf("store=%t: %d bytes/%d reuses, want %d/%d", store, len(a.B), a.IndexedBaseReuses, wantBytes, wantReuses)
		}
		if !safe && a.wordAt(before) != 0x8b160350 { // ADD X16,X26,X22
			t.Fatalf("store=%t: address reconstruction = %#08x", store, a.wordAt(before))
		}
	}
}

func BenchmarkIndexedBaseReuse(b *testing.B) {
	for _, stable := range []bool{false, true} {
		for _, safe := range []bool{false, true} {
			b.Run(fmt.Sprintf("stable=%t/safe=%t", stable, safe), func(b *testing.B) {
				a := Asm{B: make([]byte, 0, 20), DenseIdxDisp: true, ReuseIndexedBase: true}
				dst := X22
				if safe {
					dst = X0
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					a.B = a.B[:0]
					a.IndexedBaseReuses = 0
					a.LoadIdx(dst, X26, X22, 4, 1, true, true)
					if stable {
						a.Add32(X3, X3, X4)
					}
					a.StoreIdx(X26, X22, X1, 8, 4)
				}
				b.ReportMetric(float64(len(a.B)/4), "insns/op")
			})
		}
	}
}
