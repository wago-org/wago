//go:build wago_regalloccheck

package amd64

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
)

func checkGPWrites(t *testing.T, emit func(*Asm), want uint32, emits bool) {
	t.Helper()
	var a, baseline Asm
	var got uint32
	calls := 0
	a.ObserveGPWrites(func(mask uint32) {
		if mask == 0 || mask & ^uint32(0xffff) != 0 {
			t.Fatalf("invalid physical GP mask %#x", mask)
		}
		got |= mask
		calls++
	})
	emit(&a)
	emit(&baseline)
	if got != want || (calls == 0) != (want == 0) {
		t.Fatalf("writes = %#x (%d callbacks), want %#x; encoding %x", got, calls, want, a.B)
	}
	if (len(a.B) != 0) != emits {
		t.Fatalf("emitted %x, expected emission %t", a.B, emits)
	}
	if !bytes.Equal(a.B, baseline.B) {
		t.Fatalf("observer changed encoding: %x != %x", a.B, baseline.B)
	}
}

func TestGPWritesExplicitDestinations(t *testing.T) {
	for _, w := range []bool{false, true} {
		for r := RAX; r <= R15; r++ {
			t.Run(fmt.Sprintf("wide=%t/reg=%d", w, r), func(t *testing.T) {
				cases := []struct {
					name string
					emit func(*Asm)
				}{
					{"imm32", func(a *Asm) { a.MovImm32(r, 17) }},
					{"imm64-narrow", func(a *Asm) { a.MovImm64(r, 17) }},
					{"imm64-signextend", func(a *Asm) { a.MovImm64(r, ^uint64(7)) }},
					{"imm64-full", func(a *Asm) { a.MovImm64(r, 0x1234567812345678) }},
					{"mov32", func(a *Asm) { a.MovRegReg32(r, R9) }},
					{"mov64", func(a *Asm) { a.MovReg64(r, R9) }},
					{"self-mov32", func(a *Asm) { a.MovRegReg32(r, r) }},
					{"self-mov64", func(a *Asm) { a.MovReg64(r, r) }},
					{"movsxd", func(a *Asm) { a.Movsxd(r, R9) }},
					{"movsx8", func(a *Asm) { a.Movsx8(r, R9, w) }},
					{"movsx16", func(a *Asm) { a.Movsx16(r, R9, w) }},
					{"movzx8", func(a *Asm) { a.Movzx8(r, R9, w) }},
					{"movzx16", func(a *Asm) { a.Movzx16(r, R9, w) }},
					{"load32", func(a *Asm) { a.Load32(r, R12, 8) }},
					{"load64", func(a *Asm) { a.Load64(r, R12, 8) }},
					{"load-rsp32", func(a *Asm) { a.LoadRsp32(r, 8) }},
					{"load-rsp64", func(a *Asm) { a.LoadRsp64(r, 8) }},
					{"lea-rsp", func(a *Asm) { a.LeaRsp(r, 8) }},
					{"mov-rsp", func(a *Asm) { a.MovFromRsp(r) }},
					{"lea-disp", func(a *Asm) { a.LeaDisp(r, R12, 8) }},
					{"lea-disp-width", func(a *Asm) { a.LeaDispW(r, R12, 8, w) }},
					{"lea-scaled", func(a *Asm) { a.LeaScaled(r, R12, R9, 2, 8) }},
					{"lea-scaled-width", func(a *Asm) { a.LeaScaledW(r, R12, R9, 2, 8, w) }},
					{"lea-rip", func(a *Asm) { a.LeaRipPlaceholder(r) }},
					{"inc", func(a *Asm) { a.Inc(r, w) }},
					{"dec", func(a *Asm) { a.Dec(r, w) }},
					{"neg", func(a *Asm) { a.Neg(r, w) }},
					{"bswap", func(a *Asm) { a.Bswap32(r) }},
					{"bsf", func(a *Asm) { a.Bsf(r, R9, w) }},
					{"bsr", func(a *Asm) { a.Bsr(r, R9, w) }},
					{"lzcnt", func(a *Asm) { a.Lzcnt(r, R9, w) }},
					{"tzcnt", func(a *Asm) { a.Tzcnt(r, R9, w) }},
					{"popcnt", func(a *Asm) { a.Popcnt(r, R9, w) }},
					{"imul", func(a *Asm) { a.IMul(r, R9, w) }},
					{"imul-memory", func(a *Asm) { a.ImulRM(r, R12, 8, w) }},
					{"imul-index", func(a *Asm) { a.ImulIdx(r, R12, R9, 8, w) }},
					{"imul-imm8", func(a *Asm) { a.ImulRI(r, 7, w) }},
					{"imul-imm32", func(a *Asm) { a.ImulRI(r, 777, w) }},
					{"imul-three-imm8", func(a *Asm) { a.ImulRRI(r, R9, 7, w) }},
					{"imul-three-imm32", func(a *Asm) { a.ImulRRI(r, R9, 777, w) }},
					{"cmov", func(a *Asm) { a.Cmovcc(CondE, r, R9, w) }},
					{"setcc-byte", func(a *Asm) { a.SetccReg8(CondE, r) }},
					{"setcc-zeroextended", func(a *Asm) { a.SetccReg(CondE, r) }},
					{"xor-self", func(a *Asm) { a.XorSelf32(r) }},
					{"add32", func(a *Asm) { a.Add32(r, R9) }},
					{"sub32", func(a *Asm) { a.Sub32(r, R9) }},
					{"and32", func(a *Asm) { a.And32(r, R9) }},
					{"or32", func(a *Asm) { a.Or32(r, R9) }},
					{"xor32", func(a *Asm) { a.Xor32(r, R9) }},
					{"add64", func(a *Asm) { a.Add64(r, R9) }},
					{"rorx", func(a *Asm) { a.Rorx(r, R9, 7, w) }},
					{"xadd32", func(a *Asm) { a.LockXaddIdx32(R12, R9, r, 8) }},
					{"mov-xmm-gp", func(a *Asm) { a.MovXmmToGpr(r, 9, w) }},
					{"cvtt-f32", func(a *Asm) { a.Cvttf2si(r, 9, false, w) }},
					{"cvtt-f64", func(a *Asm) { a.Cvttf2si(r, 9, true, w) }},
					{"pextrb", func(a *Asm) { a.Pextrb(r, 9, 1) }},
					{"pextrw", func(a *Asm) { a.Pextrw(r, 9, 1) }},
					{"pextrd", func(a *Asm) { a.Pextrd(r, 9, 1) }},
					{"pextrq", func(a *Asm) { a.Pextrq(r, 9, 1) }},
					{"pmovmskb", func(a *Asm) { a.Pmovmskb(r, 9) }},
					{"vpmovmskb", func(a *Asm) { a.VPmovmskb(r, 9) }},
					{"vmovmskps", func(a *Asm) { a.VMovmskps(r, 9) }},
					{"vmovmskpd", func(a *Asm) { a.VMovmskpd(r, 9) }},
				}
				for _, tc := range cases {
					t.Run(tc.name, func(t *testing.T) { checkGPWrites(t, tc.emit, 1<<r, true) })
				}
				for _, size := range []int{1, 2, 4, 8} {
					for _, signed := range []bool{false, true} {
						t.Run(fmt.Sprintf("load-index/size=%d/signed=%t", size, signed), func(t *testing.T) {
							checkGPWrites(t, func(a *Asm) { a.LoadIdx(r, R12, R9, 8, size, signed, w) }, 1<<r, true)
						})
					}
					t.Run(fmt.Sprintf("atomic/size=%d", size), func(t *testing.T) {
						checkGPWrites(t, func(a *Asm) { a.LockXaddIdx(R12, R9, r, 8, size) }, 1<<r, true)
						checkGPWrites(t, func(a *Asm) { a.XchgIdx(R12, R9, r, 8, size) }, 1<<r, true)
						checkGPWrites(t, func(a *Asm) { a.LockCmpxchgIdx(R12, R9, r, 8, size) }, 1<<RAX, true)
					})
				}
				for digit := byte(0); digit < 8; digit++ {
					for _, count := range []byte{0, 1, 7} {
						want := uint32(1 << r)
						if count == 0 {
							want = 0
						}
						checkGPWrites(t, func(a *Asm) { a.ShiftImm(digit, r, count, w) }, want, count != 0)
					}
					checkGPWrites(t, func(a *Asm) { a.ShiftCL(digit, r, w) }, 1<<r, true)
				}
			})
		}
	}
}

func TestGPWritesALUOpcodeDirections(t *testing.T) {
	for _, w := range []bool{false, true} {
		for digit := byte(0); digit < 8; digit++ {
			want := uint32(1 << R13)
			if digit == 7 {
				want = 0
			}
			op := digit << 3
			checkGPWrites(t, func(a *Asm) { a.AluRR(op+1, R13, R9, w) }, want, true)
			checkGPWrites(t, func(a *Asm) { a.AluRR(op+3, R9, R13, w) }, want, true)
			checkGPWrites(t, func(a *Asm) { a.AluRR8(op, R13, R9) }, want, true)
			checkGPWrites(t, func(a *Asm) { a.AluRR8(op+2, R9, R13) }, want, true)
			checkGPWrites(t, func(a *Asm) { a.AluRM(op+3, R13, R12, 8, w) }, want, true)
			checkGPWrites(t, func(a *Asm) { a.AluIdx(op+3, R13, R12, R9, 8, w) }, want, true)
			for _, imm := range []int32{-128, 127, 128, -129, 0x12345678} {
				checkGPWrites(t, func(a *Asm) { a.AluRI(digit, R13, imm, w) }, want, true)
				accWant := uint32(0)
				if digit != 7 {
					accWant = 1 << RAX
				}
				checkGPWrites(t, func(a *Asm) { a.CompactAccumulatorImmediates = true; a.AluRI(digit, RAX, imm, w) }, accWant, true)
			}
		}
	}
	checkGPWrites(t, func(a *Asm) { a.AluRR(0x89, R13, R9, true) }, 1<<R13, true)
	checkGPWrites(t, func(a *Asm) { a.AluRR(0x8b, R9, R13, true) }, 1<<R13, true)
	checkGPWrites(t, func(a *Asm) { a.AluRR(0x87, R13, R9, true) }, 1<<R13|1<<R9, true)
	checkGPWrites(t, func(a *Asm) { a.AluRR8(0x88, R13, R9) }, 1<<R13, true)
	checkGPWrites(t, func(a *Asm) { a.AluRR8(0x8a, R9, R13) }, 1<<R13, true)
	checkGPWrites(t, func(a *Asm) { a.AluRR8(0x86, R13, R9) }, 1<<R13|1<<R9, true)
}

func TestGPWritesImplicitDestinations(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		mask uint32
	}{
		{"push", func(a *Asm) { a.Push(R13) }, 1 << RSP},
		{"pop", func(a *Asm) { a.Pop(R13) }, 1<<RSP | 1<<R13},
		{"pop-rsp", func(a *Asm) { a.Pop(RSP) }, 1 << RSP},
		{"leave", func(a *Asm) { a.Leave() }, 1<<RSP | 1<<RBP},
		{"ret", func(a *Asm) { a.Ret() }, 1 << RSP},
		{"prologue", func(a *Asm) { a.Prologue() }, 1<<RSP | 1<<RBP},
		{"sub-rsp", func(a *Asm) { a.SubRsp(16) }, 1 << RSP},
		{"add-rsp", func(a *Asm) { a.AddRsp(16) }, 1 << RSP},
		{"set-al", func(a *Asm) { a.SetccAL(CondE) }, 1 << RAX},
		{"xchg", func(a *Asm) { a.Xchg64(R13, R9) }, 1<<R13 | 1<<R9},
		{"xchg-self", func(a *Asm) { a.Xchg64(R13, R13) }, 1 << R13},
		{"rep-movsb", func(a *Asm) { a.RepMovsb() }, 1<<RCX | 1<<RSI | 1<<RDI},
		{"rep-stosb", func(a *Asm) { a.RepStosb() }, 1<<RCX | 1<<RDI},
		{"call-relative", func(a *Asm) { a.CallRel32() }, 0xffff},
		{"call-register", func(a *Asm) { a.CallReg(R13) }, 0xffff},
		{"call-memory", func(a *Asm) { a.CallMem(R13, 8) }, 0xffff},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkGPWrites(t, tc.emit, tc.mask, true) })
	}
	for _, w := range []bool{false, true} {
		checkGPWrites(t, func(a *Asm) { a.Cdq(w) }, 1<<RDX, true)
		checkGPWrites(t, func(a *Asm) { a.Idiv(R13, w) }, 1<<RAX|1<<RDX, true)
		checkGPWrites(t, func(a *Asm) { a.Div(R13, w) }, 1<<RAX|1<<RDX, true)
		checkGPWrites(t, func(a *Asm) { a.Mul(R13, w) }, 1<<RAX|1<<RDX, true)
		checkGPWrites(t, func(a *Asm) { a.IMulHigh(R13, w) }, 1<<RAX|1<<RDX, true)
	}
}

// Exercise raw selectors as well as named wrappers: backend simdUnaryOp.emit
// uses SseMapRR/VexMapRR directly for masks, bypassing the named methods.
func TestGPWritesGenericSIMDDestinations(t *testing.T) {
	cases := []struct {
		name                      string
		prefix, pp, opcodeMap, op byte
		rmDestination, immediate  bool
	}{
		{"cvttss", 0xf3, 2, 0, 0x2c, false, false},
		{"cvttsd", 0xf2, 3, 0, 0x2c, false, false},
		{"cvtss", 0xf3, 2, 0, 0x2d, false, false},
		{"cvtsd", 0xf2, 3, 0, 0x2d, false, false},
		{"movmskps", 0, 0, 0, 0x50, false, false},
		{"movmskpd", 0x66, 1, 0, 0x50, false, false},
		{"pmovmskb", 0x66, 1, 0, 0xd7, false, false},
		{"pextrw-reg", 0x66, 1, 0, 0xc5, false, true},
		{"movd", 0x66, 1, 0, 0x7e, true, false},
		{"pextrb", 0x66, 1, 0x3a, 0x14, true, true},
		{"pextrw-rm", 0x66, 1, 0x3a, 0x15, true, true},
		{"pextrd", 0x66, 1, 0x3a, 0x16, true, true},
		{"extractps", 0x66, 1, 0x3a, 0x17, true, true},
	}
	for r := RAX; r <= R15; r++ {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/reg=%d", tc.name, r), func(t *testing.T) {
				reg, rm := r, Reg(9)
				if tc.rmDestination {
					reg, rm = rm, reg
				}
				checkGPWrites(t, func(a *Asm) {
					if tc.immediate {
						a.SseMapRRI(tc.prefix, tc.opcodeMap, tc.op, reg, rm, 1)
					} else {
						a.SseMapRR(tc.prefix, tc.opcodeMap, tc.op, reg, rm)
					}
				}, 1<<r, true)
				checkGPWrites(t, func(a *Asm) {
					a.VexMapRR(tc.opcodeMap, tc.pp, tc.op, reg, rm)
					if tc.immediate {
						a.emit(1)
					}
				}, 1<<r, true)
				checkGPWrites(t, func(a *Asm) {
					a.VexMapRRR(tc.opcodeMap, tc.pp, tc.op, reg, 0, rm)
					if tc.immediate {
						a.emit(1)
					}
				}, 1<<r, true)
			})
		}
		checkGPWrites(t, func(a *Asm) { a.YPmovmskb(r, 9) }, 1<<r, true)
		for _, w := range []bool{false, true} {
			checkGPWrites(t, func(a *Asm) { a.SseRR(0x66, 0x7e, 9, r, w) }, 1<<r, true)
			checkGPWrites(t, func(a *Asm) { a.vex3RRReservedWL(vexMap0F, 1, 0x7e, 9, r, w, 0) }, 1<<r, true)
		}
	}
	for _, f64 := range []bool{false, true} {
		for _, op := range []byte{0x2c, 0x2d} {
			checkGPWrites(t, func(a *Asm) { a.SseIdx(sdPrefix(f64), op, R13, R12, R9, 8) }, 1<<R13, true)
			checkGPWrites(t, func(a *Asm) { a.FAluDisp(op, R13, R12, 8, f64) }, 1<<R13, true)
			checkGPWrites(t, func(a *Asm) { a.fmemIdx(op, R13, R12, R9, 8, f64) }, 1<<R13, true)
			checkGPWrites(t, func(a *Asm) { a.VSseMemDisp(vexPP(f64), op, R13, 0, R12, 8) }, 1<<R13, true)
			checkGPWrites(t, func(a *Asm) { a.VFMemIdx(op, R13, 0, R12, R9, 8, f64) }, 1<<R13, true)
			checkGPWrites(t, func(a *Asm) { a.vex3MemRipPlaceholder(vexMap0F, vexPP(f64), op, R13, 0) }, 1<<R13, true)
		}
	}
}

func TestGPWritesReadOnlyAndFPOnly(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
	}{
		{"cmp32", func(a *Asm) { a.Cmp32(R13, R9) }},
		{"cmp64", func(a *Asm) { a.Cmp64(R13, R9) }},
		{"test", func(a *Asm) { a.TestReg(R13, R9, true) }},
		{"test-self", func(a *Asm) { a.TestSelf(R13, true) }},
		{"test-imm", func(a *Asm) { a.TestImm(R13, 7, true) }},
		{"test-acc-imm", func(a *Asm) { a.CompactAccumulatorImmediates = true; a.TestImm(RAX, 7, true) }},
		{"test-generic", func(a *Asm) { a.AluRR(0x85, R13, R9, true) }},
		{"test-byte", func(a *Asm) { a.AluRR8(0x84, R13, R9) }},
		{"bit-test", func(a *Asm) { a.BtImm(R13, 7, true) }},
		{"compare-memory", func(a *Asm) { a.CmpImmIdx(R13, R9, 8, 7, 8) }},
		{"store32", func(a *Asm) { a.Store32(R12, 8, R13) }},
		{"store64", func(a *Asm) { a.Store64(R12, 8, R13) }},
		{"store-rsp32", func(a *Asm) { a.StoreRsp32(8, R13) }},
		{"store-rsp64", func(a *Asm) { a.StoreRsp64(8, R13) }},
		{"store-imm", func(a *Asm) { a.StoreImm32Mem(R13, 8, 7) }},
		{"store-imm-index", func(a *Asm) { a.StoreImmIdx(R12, R9, 8, 7, 4) }},
		{"store-index", func(a *Asm) { a.StoreIdx(R12, R9, R13, 8, 8) }},
		{"atomic-memory-only", func(a *Asm) { a.LockAluIdx(0x21, R12, R9, R13, 8, 8) }},
		{"mfence", func(a *Asm) { a.Mfence() }},
		{"std", func(a *Asm) { a.Std() }},
		{"cld", func(a *Asm) { a.Cld() }},
		{"branch-relative", func(a *Asm) { a.JmpPlaceholder() }},
		{"branch-conditional", func(a *Asm) { a.JccPlaceholder(CondE) }},
		{"branch-rcx", func(a *Asm) { a.JcxzPlaceholder(true) }},
		{"branch-ecx", func(a *Asm) { a.JcxzPlaceholder(false) }},
		{"branch-short", func(a *Asm) { a.JccRel8(CondE, 0) }},
		{"branch-back", func(a *Asm) { a.JmpBack(0) }},
		{"branch-register", func(a *Asm) { a.JmpReg(R13) }},
		{"gp-to-fp", func(a *Asm) { a.MovGprToXmm(13, R9, true) }},
		{"convert-gp-to-fp", func(a *Asm) { a.Cvtsi2f(13, R9, true, true) }},
		{"scalar-fp", func(a *Asm) { a.FAdd(13, 9, true) }},
		{"scalar-fp-move", func(a *Asm) { a.FMov(13, 9, true) }},
		{"fp-compare", func(a *Asm) { a.Ucomis(13, 9, true) }},
		{"fp-load", func(a *Asm) { a.FLoadDisp(13, R12, 8, true) }},
		{"fp-store", func(a *Asm) { a.FStoreDisp(R12, 8, 13, true) }},
		{"fp-index-load", func(a *Asm) { a.FLoadIdx(13, R12, R9, 8, true) }},
		{"fp-index-store", func(a *Asm) { a.FStoreIdx(R12, R9, 13, 8, true) }},
		{"sse-movq-fp", func(a *Asm) { a.SseMapRR(0xf3, 0, 0x7e, 13, 9) }},
		{"vex-movq-fp", func(a *Asm) { a.VexMapRR(0, 2, 0x7e, 13, 9) }},
		{"sse-packed-cvtt", func(a *Asm) { a.SseMapRR(0x66, 0, 0x2c, 5, 9) }},
		{"sse-cvttps", func(a *Asm) { a.SseMapRR(0xf3, 0, 0x5b, 13, 9) }},
		{"vex-cvttps", func(a *Asm) { a.Vcvttps2dq(13, 9) }},
		{"vex-cvttpd", func(a *Asm) { a.Vcvttpd2dq(13, 9) }},
		{"sse-insert", func(a *Asm) { a.Pinsrq(13, R9, 1) }},
		{"sse-test", func(a *Asm) { a.SseMapRR(0x66, 0x38, 0x17, 13, 9) }},
		{"vex-test", func(a *Asm) { a.VPtest(13, 9) }},
		{"vex-packed", func(a *Asm) { a.VexMapRRR(0, 1, 0xfe, 13, 9, 7) }},
		{"vex-shift", func(a *Asm) { a.VexShiftImm(0x72, 6, 13, 9, 7) }},
		{"sse-shift", func(a *Asm) { a.SseMapRRI(0x66, 0, 0x72, 6, 13, 7) }},
		{"ymm-packed", func(a *Asm) { a.YPaddd(13, 9, 7) }},
		{"evex-packed", func(a *Asm) { a.ZSIMDRRR(vexMap0F, 1, 0xfe, false, 13, 9, 7) }},
		{"zero-upper", func(a *Asm) { a.VZeroUpper() }},
		{"simd-load", func(a *Asm) { a.MovdquLoadDisp(13, R12, 8) }},
		{"simd-store", func(a *Asm) { a.MovdquStoreDisp(R12, 8, 13) }},
		{"simd-vex-load", func(a *Asm) { a.VMovdquLoadDisp(13, R12, 8) }},
		{"simd-vex-store", func(a *Asm) { a.VMovdquStoreDisp(R12, 8, 13) }},
		{"simd-vex-index", func(a *Asm) { a.VMovdquIdx(0x6f, 13, R12, R9, 8) }},
		{"simd-movd-memory", func(a *Asm) { a.SseIdx(0x66, 0x7e, 13, R12, R9, 8) }},
		{"simd-vex-movd-memory", func(a *Asm) { a.vex3MemDisp(vexMap0F, 1, 0x7e, 13, 0, false, R12, 8) }},
		{"simd-vex-extract-memory", func(a *Asm) { a.vex3MemIdx(vexMap0F3A, 1, 0x16, 13, 0, false, R12, R9, 8); a.emit(1) }},
		{"padding", func(a *Asm) { a.emit(0x90); a.Align16(); a.AlignLoop(); a.AlignLoop32() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkGPWrites(t, tc.emit, 0, true) })
	}
	checkGPWrites(t, func(a *Asm) { a.ShiftImm(4, R13, 0, true) }, 0, false)
	checkGPWrites(t, func(a *Asm) { a.JccRel8(CondE, 1000) }, 0, false)
	checkGPWrites(t, func(a *Asm) { a.Align16(); a.AlignLoop(); a.AlignLoop32() }, 0, false)
}

func TestGPWritesObserverScopesAndTransferIndependence(t *testing.T) {
	var a Asm
	var outer, inner uint32
	var effects []regalloccheck.Effect
	a.ObserveRegalloc(func(e regalloccheck.Effect) { effects = append(effects, e) })
	if old := a.ObserveGPWrites(func(mask uint32) { outer |= mask }); old != nil {
		t.Fatal("non-nil initial observer")
	}
	a.Add64(R13, R9)
	a.SetccReg8(CondE, R12)
	if len(effects) != 0 {
		t.Fatalf("physical writes changed transfer semantics: %+v", effects)
	}
	old := a.ObserveGPWrites(func(mask uint32) { inner |= mask })
	a.MovReg64(R8, R9)
	if len(effects) != 1 || effects[0].Kind != regalloccheck.Copy {
		t.Fatalf("transfer observer lost: %+v", effects)
	}
	a.ObserveGPWrites(old)
	a.Add32(R14, R9)
	a.ObserveRegalloc(nil)
	a.Add32(R15, R9)
	if inner != 1<<R8 || outer != 1<<R12|1<<R13|1<<R14|1<<R15 {
		t.Fatalf("scoped writes: inner=%#x outer=%#x", inner, outer)
	}
	a.ObserveGPWrites(nil)
	a.Add32(RAX, R9)
	if outer&(1<<RAX) != 0 {
		t.Fatal("observer was not disabled")
	}
}

func TestGPWritesLegacyHighByteAliases(t *testing.T) {
	// The generic opcode helpers can encode legacy high-byte operands. Report
	// the physical container selected by the emitted REX state, not its field.
	for field := Reg(4); field < 8; field++ {
		checkGPWrites(t, func(a *Asm) { a.AluRR(0x00, field, RAX, false) }, 1<<(field-4), true)
		checkGPWrites(t, func(a *Asm) { a.AluRR(0x02, RAX, field, false) }, 1<<(field-4), true)
		checkGPWrites(t, func(a *Asm) { a.AluRR(0x88, field, RAX, false) }, 1<<(field-4), true)
		checkGPWrites(t, func(a *Asm) { a.AluRM(0x02, field, RAX, 8, false) }, 1<<(field-4), true)
		checkGPWrites(t, func(a *Asm) { a.AluIdx(0x02, field, RAX, RCX, 8, false) }, 1<<(field-4), true)
		checkGPWrites(t, func(a *Asm) { a.AluRR(0x00, field, R8, false) }, 1<<field, true)
		checkGPWrites(t, func(a *Asm) { a.AluRR8(0x00, field, RAX) }, 1<<field, true)
		checkGPWrites(t, func(a *Asm) { a.AluRM(0x02, field, R8, 8, false) }, 1<<field, true)
		checkGPWrites(t, func(a *Asm) { a.AluIdx(0x02, field, RAX, R8, 8, false) }, 1<<field, true)
	}
}

func TestGPWritesWaitForFoldedReadValidation(t *testing.T) {
	for _, emit := range []func(*Asm){
		func(a *Asm) { a.AluRM(0x03, R13, RSP, 8, true) },
		func(a *Asm) { a.ImulRM(R13, RSP, 8, true) },
	} {
		var a Asm
		a.ObserveGPWrites(func(uint32) { t.Fatal("write reported before folded input validation") })
		a.ObserveRegalloc(func(e regalloccheck.Effect) {
			if e.Kind == regalloccheck.Read {
				panic("rejected input")
			}
		})
		func() {
			defer func() {
				if got := recover(); got != "rejected input" {
					t.Fatalf("unexpected panic %v", got)
				}
			}()
			emit(&a)
		}()
		if len(a.B) != 0 {
			t.Fatalf("rejected instruction emitted %x", a.B)
		}
	}
}
