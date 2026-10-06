//go:build wago_regalloccheck

package arm64

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
)

// Every callback is tied to emitted bytes, and no empty write is reported.
func recordGPWrites(t *testing.T, a *Asm) *[]uint32 {
	t.Helper()
	var got []uint32
	at := a.Len()
	a.ObserveGPWrites(func(mask uint32) {
		if mask == 0 || a.Len() <= at {
			t.Errorf("write mask %#x without a newly emitted instruction at %d", mask, a.Len())
		}
		at = a.Len()
		got = append(got, mask)
	})
	return &got
}

func TestGPWritesSingleDestination(t *testing.T) {
	tests := []struct {
		name string
		emit func(*Asm, Reg)
		sp   bool
	}{
		{"Add64", func(a *Asm, d Reg) { a.Add64(d, X6, X7) }, false},
		{"Add32", func(a *Asm, d Reg) { a.Add32(d, X6, X7) }, false},
		{"Sub64", func(a *Asm, d Reg) { a.Sub64(d, X6, X7) }, false},
		{"Sub32", func(a *Asm, d Reg) { a.Sub32(d, X6, X7) }, false},
		{"Adds64", func(a *Asm, d Reg) { a.Adds64(d, X6, X7) }, false},
		{"Subs64", func(a *Asm, d Reg) { a.Subs64(d, X6, X7) }, false},
		{"Adds32", func(a *Asm, d Reg) { a.Adds32(d, X6, X7) }, false},
		{"And32", func(a *Asm, d Reg) { a.And32(d, X6, X7) }, false},
		{"Orr32", func(a *Asm, d Reg) { a.Orr32(d, X6, X7) }, false},
		{"Eor32", func(a *Asm, d Reg) { a.Eor32(d, X6, X7) }, false},
		{"And64", func(a *Asm, d Reg) { a.And64(d, X6, X7) }, false},
		{"Orr64", func(a *Asm, d Reg) { a.Orr64(d, X6, X7) }, false},
		{"Eor64", func(a *Asm, d Reg) { a.Eor64(d, X6, X7) }, false},
		{"Lslv32", func(a *Asm, d Reg) { a.Lslv32(d, X6, X7) }, false},
		{"Lsrv32", func(a *Asm, d Reg) { a.Lsrv32(d, X6, X7) }, false},
		{"Asrv32", func(a *Asm, d Reg) { a.Asrv32(d, X6, X7) }, false},
		{"Lslv64", func(a *Asm, d Reg) { a.Lslv64(d, X6, X7) }, false},
		{"Lsrv64", func(a *Asm, d Reg) { a.Lsrv64(d, X6, X7) }, false},
		{"Asrv64", func(a *Asm, d Reg) { a.Asrv64(d, X6, X7) }, false},
		{"Mul64", func(a *Asm, d Reg) { a.Mul64(d, X6, X7) }, false},
		{"Mul32", func(a *Asm, d Reg) { a.Mul32(d, X6, X7) }, false},
		{"Rorv32", func(a *Asm, d Reg) { a.Rorv32(d, X6, X7) }, false},
		{"Rorv64", func(a *Asm, d Reg) { a.Rorv64(d, X6, X7) }, false},
		{"Sdiv32", func(a *Asm, d Reg) { a.Sdiv32(d, X6, X7) }, false},
		{"Sdiv64", func(a *Asm, d Reg) { a.Sdiv64(d, X6, X7) }, false},
		{"Udiv32", func(a *Asm, d Reg) { a.Udiv32(d, X6, X7) }, false},
		{"Udiv64", func(a *Asm, d Reg) { a.Udiv64(d, X6, X7) }, false},
		{"Smulh", func(a *Asm, d Reg) { a.Smulh(d, X6, X7) }, false},
		{"Umulh", func(a *Asm, d Reg) { a.Umulh(d, X6, X7) }, false},
		{"Smull", func(a *Asm, d Reg) { a.Smull(d, X6, X7) }, false},
		{"Umull", func(a *Asm, d Reg) { a.Umull(d, X6, X7) }, false},
		{"AddExtUXTW", func(a *Asm, d Reg) { a.AddExtUXTW(d, SP, X7) }, true},
		{"AddImm64", func(a *Asm, d Reg) { a.AddImm64(d, X6, 9) }, true},
		{"AddImm32", func(a *Asm, d Reg) { a.AddImm32(d, X6, 9) }, true},
		{"SubImm64", func(a *Asm, d Reg) { a.SubImm64(d, X6, 9) }, true},
		{"SubImm32", func(a *Asm, d Reg) { a.SubImm32(d, X6, 9) }, true},
		{"SubsImm64", func(a *Asm, d Reg) { a.SubsImm64(d, X6, 9) }, false},
		{"SubsImm32", func(a *Asm, d Reg) { a.SubsImm32(d, X6, 9) }, false},
		{"AddImm64LSL12", func(a *Asm, d Reg) { a.AddImm64LSL12(d, SP, 4096) }, true},
		{"AddImm32LSL12", func(a *Asm, d Reg) { a.AddImm32LSL12(d, SP, 4096) }, true},
		{"SubImm64LSL12", func(a *Asm, d Reg) { a.SubImm64LSL12(d, SP, 4096) }, true},
		{"SubImm32LSL12", func(a *Asm, d Reg) { a.SubImm32LSL12(d, SP, 4096) }, true},
		{"MovReg64", func(a *Asm, d Reg) { a.MovReg64(d, X6) }, false},
		{"MovReg32", func(a *Asm, d Reg) { a.MovReg32(d, X6) }, false},
		{"Sxtw", func(a *Asm, d Reg) { a.Sxtw(d, X6) }, false},
		{"Movz64", func(a *Asm, d Reg) { a.Movz64(d, 123, 1) }, false},
		{"Movk64", func(a *Asm, d Reg) { a.Movk64(d, 123, 1) }, false},
		{"Movn64", func(a *Asm, d Reg) { a.Movn64(d, 123, 1) }, false},
		{"Movz32", func(a *Asm, d Reg) { a.Movz32(d, 123, 1) }, false},
		{"Movk32", func(a *Asm, d Reg) { a.Movk32(d, 123, 1) }, false},
		{"Movn32", func(a *Asm, d Reg) { a.Movn32(d, 123, 1) }, false},
		{"Madd64", func(a *Asm, d Reg) { a.Madd64(d, X6, X7, X8) }, false},
		{"Madd32", func(a *Asm, d Reg) { a.Madd32(d, X6, X7, X8) }, false},
		{"Msub64", func(a *Asm, d Reg) { a.Msub64(d, X6, X7, X8) }, false},
		{"Msub32", func(a *Asm, d Reg) { a.Msub32(d, X6, X7, X8) }, false},
		{"Csel64", func(a *Asm, d Reg) { a.Csel64(d, X6, X7, CondEQ) }, false},
		{"Csel32", func(a *Asm, d Reg) { a.Csel32(d, X6, X7, CondEQ) }, false},
		{"Csinc32", func(a *Asm, d Reg) { a.Csinc32(d, X6, X7, CondEQ) }, false},
		{"Cset64", func(a *Asm, d Reg) { a.Cset64(d, CondNE) }, false},
		{"Cset32", func(a *Asm, d Reg) { a.Cset32(d, CondNE) }, false},
		{"AndImm64", func(a *Asm, d Reg) { a.AndImm64(d, X6, 255) }, true},
		{"OrrImm64", func(a *Asm, d Reg) { a.OrrImm64(d, X6, 255) }, true},
		{"EorImm64", func(a *Asm, d Reg) { a.EorImm64(d, X6, 255) }, true},
		{"AndImm32", func(a *Asm, d Reg) { a.AndImm32(d, X6, 255) }, true},
		{"OrrImm32", func(a *Asm, d Reg) { a.OrrImm32(d, X6, 255) }, true},
		{"EorImm32", func(a *Asm, d Reg) { a.EorImm32(d, X6, 255) }, true},
		{"Sxtb/false", func(a *Asm, d Reg) { a.Sxtb(d, X6, false) }, false},
		{"Sxth/false", func(a *Asm, d Reg) { a.Sxth(d, X6, false) }, false},
		{"Clz/false", func(a *Asm, d Reg) { a.Clz(d, X6, false) }, false},
		{"Rbit/false", func(a *Asm, d Reg) { a.Rbit(d, X6, false) }, false},
		{"FmovToGpr/false", func(a *Asm, d Reg) { a.FmovToGpr(d, X6, false) }, false},
		{"LslImm/false", func(a *Asm, d Reg) { a.LslImm(d, X6, 3, false) }, false},
		{"LsrImm/false", func(a *Asm, d Reg) { a.LsrImm(d, X6, 3, false) }, false},
		{"AsrImm/false", func(a *Asm, d Reg) { a.AsrImm(d, X6, 3, false) }, false},
		{"RorImm/false", func(a *Asm, d Reg) { a.RorImm(d, X6, 3, false) }, false},
		{"AddShifted/false", func(a *Asm, d Reg) { a.AddShifted(d, X6, X7, 3, false) }, false},
		{"AddShiftedReg/false", func(a *Asm, d Reg) { a.AddShiftedReg(d, X6, X7, RegShiftLSR, 3, false) }, false},
		{"SubShiftedReg/false", func(a *Asm, d Reg) { a.SubShiftedReg(d, X6, X7, RegShiftLSR, 3, false) }, false},
		{"AndShiftedReg/false", func(a *Asm, d Reg) { a.AndShiftedReg(d, X6, X7, RegShiftLSR, 3, false) }, false},
		{"OrrShiftedReg/false", func(a *Asm, d Reg) { a.OrrShiftedReg(d, X6, X7, RegShiftLSR, 3, false) }, false},
		{"EorShiftedReg/false", func(a *Asm, d Reg) { a.EorShiftedReg(d, X6, X7, RegShiftLSR, 3, false) }, false},
		{"Csel/false", func(a *Asm, d Reg) { a.Csel(d, X6, X7, CondEQ, false) }, false},
		{"Fcvtzs/false/false", func(a *Asm, d Reg) { a.Fcvtzs(d, X6, false, false) }, false},
		{"Fcvtzs/false/true", func(a *Asm, d Reg) { a.Fcvtzs(d, X6, false, true) }, false},
		{"Sxtb/true", func(a *Asm, d Reg) { a.Sxtb(d, X6, true) }, false},
		{"Sxth/true", func(a *Asm, d Reg) { a.Sxth(d, X6, true) }, false},
		{"Clz/true", func(a *Asm, d Reg) { a.Clz(d, X6, true) }, false},
		{"Rbit/true", func(a *Asm, d Reg) { a.Rbit(d, X6, true) }, false},
		{"FmovToGpr/true", func(a *Asm, d Reg) { a.FmovToGpr(d, X6, true) }, false},
		{"LslImm/true", func(a *Asm, d Reg) { a.LslImm(d, X6, 3, true) }, false},
		{"LsrImm/true", func(a *Asm, d Reg) { a.LsrImm(d, X6, 3, true) }, false},
		{"AsrImm/true", func(a *Asm, d Reg) { a.AsrImm(d, X6, 3, true) }, false},
		{"RorImm/true", func(a *Asm, d Reg) { a.RorImm(d, X6, 3, true) }, false},
		{"AddShifted/true", func(a *Asm, d Reg) { a.AddShifted(d, X6, X7, 3, true) }, false},
		{"AddShiftedReg/true", func(a *Asm, d Reg) { a.AddShiftedReg(d, X6, X7, RegShiftLSR, 3, true) }, false},
		{"SubShiftedReg/true", func(a *Asm, d Reg) { a.SubShiftedReg(d, X6, X7, RegShiftLSR, 3, true) }, false},
		{"AndShiftedReg/true", func(a *Asm, d Reg) { a.AndShiftedReg(d, X6, X7, RegShiftLSR, 3, true) }, false},
		{"OrrShiftedReg/true", func(a *Asm, d Reg) { a.OrrShiftedReg(d, X6, X7, RegShiftLSR, 3, true) }, false},
		{"EorShiftedReg/true", func(a *Asm, d Reg) { a.EorShiftedReg(d, X6, X7, RegShiftLSR, 3, true) }, false},
		{"Csel/true", func(a *Asm, d Reg) { a.Csel(d, X6, X7, CondEQ, true) }, false},
		{"Fcvtzs/true/false", func(a *Asm, d Reg) { a.Fcvtzs(d, X6, true, false) }, false},
		{"Fcvtzs/true/true", func(a *Asm, d Reg) { a.Fcvtzs(d, X6, true, true) }, false},
		{"LslImm64", func(a *Asm, d Reg) { a.LslImm64(d, X6, 3) }, false},
		{"LsrImm32", func(a *Asm, d Reg) { a.LsrImm32(d, X6, 3) }, false},
		{"AsrImm64", func(a *Asm, d Reg) { a.AsrImm64(d, X6, 3) }, false},
		{"Load64", func(a *Asm, d Reg) { a.Load64(d, SP, 8) }, false},
		{"Load32", func(a *Asm, d Reg) { a.Load32(d, SP, 8) }, false},
		{"Ldrb", func(a *Asm, d Reg) { a.Ldrb(d, SP, 8) }, false},
		{"Ldrh", func(a *Asm, d Reg) { a.Ldrh(d, SP, 8) }, false},
		{"Ldur64", func(a *Asm, d Reg) { a.Ldur64(d, SP, 8) }, false},
		{"Ldur32", func(a *Asm, d Reg) { a.Ldur32(d, SP, 8) }, false},
		{"Ldaxr32", func(a *Asm, d Reg) { a.Ldaxr32(d, X6) }, false},
		{"Stlxr32", func(a *Asm, d Reg) { a.Stlxr32(d, X7, SP) }, false},
		{"Ldaxr/1", func(a *Asm, d Reg) { a.Ldaxr(d, SP, 1) }, false},
		{"Ldar/1", func(a *Asm, d Reg) { a.Ldar(d, SP, 1) }, false},
		{"Stlxr/1", func(a *Asm, d Reg) { a.Stlxr(d, X7, SP, 1) }, false},
		{"LdrIdx/unsigned/1", func(a *Asm, d Reg) { a.LdrIdx(d, SP, X7, 1, false, true) }, false},
		{"LoadIdx/zero/1", func(a *Asm, d Reg) { a.LoadIdx(d, SP, X7, 0, 1, false, true) }, false},
		{"LdrIdx/signed/1/False", func(a *Asm, d Reg) { a.LdrIdx(d, SP, X7, 1, true, false) }, false},
		{"LdrIdx/signed/1/True", func(a *Asm, d Reg) { a.LdrIdx(d, SP, X7, 1, true, true) }, false},
		{"Ldaxr/2", func(a *Asm, d Reg) { a.Ldaxr(d, SP, 2) }, false},
		{"Ldar/2", func(a *Asm, d Reg) { a.Ldar(d, SP, 2) }, false},
		{"Stlxr/2", func(a *Asm, d Reg) { a.Stlxr(d, X7, SP, 2) }, false},
		{"LdrIdx/unsigned/2", func(a *Asm, d Reg) { a.LdrIdx(d, SP, X7, 2, false, true) }, false},
		{"LoadIdx/zero/2", func(a *Asm, d Reg) { a.LoadIdx(d, SP, X7, 0, 2, false, true) }, false},
		{"LdrIdx/signed/2/False", func(a *Asm, d Reg) { a.LdrIdx(d, SP, X7, 2, true, false) }, false},
		{"LdrIdx/signed/2/True", func(a *Asm, d Reg) { a.LdrIdx(d, SP, X7, 2, true, true) }, false},
		{"Ldaxr/4", func(a *Asm, d Reg) { a.Ldaxr(d, SP, 4) }, false},
		{"Ldar/4", func(a *Asm, d Reg) { a.Ldar(d, SP, 4) }, false},
		{"Stlxr/4", func(a *Asm, d Reg) { a.Stlxr(d, X7, SP, 4) }, false},
		{"LdrIdx/unsigned/4", func(a *Asm, d Reg) { a.LdrIdx(d, SP, X7, 4, false, true) }, false},
		{"LoadIdx/zero/4", func(a *Asm, d Reg) { a.LoadIdx(d, SP, X7, 0, 4, false, true) }, false},
		{"LdrIdx/signed/4/True", func(a *Asm, d Reg) { a.LdrIdx(d, SP, X7, 4, true, true) }, false},
		{"Ldaxr/8", func(a *Asm, d Reg) { a.Ldaxr(d, SP, 8) }, false},
		{"Ldar/8", func(a *Asm, d Reg) { a.Ldar(d, SP, 8) }, false},
		{"Stlxr/8", func(a *Asm, d Reg) { a.Stlxr(d, X7, SP, 8) }, false},
		{"LdrIdx/unsigned/8", func(a *Asm, d Reg) { a.LdrIdx(d, SP, X7, 8, false, true) }, false},
		{"LoadIdx/zero/8", func(a *Asm, d Reg) { a.LoadIdx(d, SP, X7, 0, 8, false, true) }, false},
		{"NeonUmovB", func(a *Asm, d Reg) { a.NeonUmovB(d, 31, 1) }, false},
		{"NeonUmovH", func(a *Asm, d Reg) { a.NeonUmovH(d, 31, 1) }, false},
		{"NeonUmovS", func(a *Asm, d Reg) { a.NeonUmovS(d, 31, 1) }, false},
		{"NeonUmovD", func(a *Asm, d Reg) { a.NeonUmovD(d, 31, 1) }, false},
		{"Adr", func(a *Asm, d Reg) { a.Adr(d) }, false},
	}
	for _, tt := range tests {
		for _, dst := range []Reg{X0, X9, X16, X17, LR, XZR, 41, 63} {
			t.Run(fmt.Sprintf("%s/r%d", tt.name, dst), func(t *testing.T) {
				var a Asm
				got := recordGPWrites(t, &a)
				tt.emit(&a, dst)
				if a.Len() != 4 {
					t.Fatalf("got %d bytes, want one instruction", a.Len())
				}
				// Derive the expected physical register independently of the hook.
				physical := uint32(dst) & 31
				var want []uint32
				if physical != 31 || tt.sp {
					want = []uint32{1 << physical}
				}
				if !reflect.DeepEqual(*got, want) {
					t.Fatalf("writes %#x, want %#x", *got, want)
				}
			})
		}
	}
}

func TestGPWritesImplicitAndPairDestinations(t *testing.T) {
	tests := []struct {
		name string
		emit func(*Asm)
		want []uint32
	}{
		{"BL", func(a *Asm) { a.Bl() }, []uint32{^uint32(0)}},
		{"BLR", func(a *Asm) { a.Blr(X6) }, []uint32{^uint32(0)}},
		{"AddSP64", func(a *Asm) { a.AddSP64(16) }, []uint32{1 << 31}},
		{"SubSP64", func(a *Asm) { a.SubSP64(16) }, []uint32{1 << 31}},
		{"AddSPReg", func(a *Asm) { a.AddSPReg(X6) }, []uint32{1 << 31}},
		{"SubSPReg", func(a *Asm) { a.SubSPReg(X6) }, []uint32{1 << 31}},
		{"StpPreSP", func(a *Asm) { a.StpPre(X6, X7, SP, -16) }, []uint32{1 << 31}},
		{"StpPreBase", func(a *Asm) { a.StpPre(X6, X7, X8, -16) }, []uint32{1 << 8}},
		{"LdpOffset", func(a *Asm) { a.LdpOffset(X6, X7, SP, 16) }, []uint32{1<<6 | 1<<7}},
		{"LdpOffset32", func(a *Asm) { a.LdpOffset32(X6, X7, SP, 16) }, []uint32{1<<6 | 1<<7}},
		{"LdpPostSP", func(a *Asm) { a.LdpPost(X6, X7, SP, 16) }, []uint32{1<<6 | 1<<7 | 1<<31}},
		{"LdpPostBase", func(a *Asm) { a.LdpPost(X6, X7, X8, 16) }, []uint32{1<<6 | 1<<7 | 1<<8}},
		{"LdpFirstZR", func(a *Asm) { a.LdpOffset(XZR, X7, SP, 16) }, []uint32{1 << 7}},
		{"LdpSecondZR", func(a *Asm) { a.LdpOffset(X6, XZR, SP, 16) }, []uint32{1 << 6}},
		{"LdpBothZR", func(a *Asm) { a.LdpOffset(XZR, XZR, SP, 16) }, nil},
		{"LdpPostBothZR", func(a *Asm) { a.LdpPost(XZR, XZR, SP, 16) }, []uint32{1 << 31}},
		{"LoadPairIdx32", func(a *Asm) { a.LoadPairIdx(X6, X7, X8, X9, 16, 4) }, []uint32{1 << 16, 1<<6 | 1<<7}},
		{"LoadPairIdx64", func(a *Asm) { a.LoadPairIdx(X6, X7, X8, X9, 16, 8) }, []uint32{1 << 16, 1<<6 | 1<<7}},
		{"NeonMovemaskB", func(a *Asm) { a.NeonMovemaskB(X6, 31) }, []uint32{1 << 6, 1 << 6, 1 << 6}},
		{"LeaSPImmediate", func(a *Asm) { a.LeaSP(X6, 16) }, []uint32{1 << 6}},
		{"LeaSPShifted", func(a *Asm) { a.LeaSP(X6, -4096) }, []uint32{1 << 6}},
		{"LeaSPExtended", func(a *Asm) { a.LeaSP(X6, 0x12345) }, []uint32{1 << 16, 1 << 16, 1 << 6}},
		{"LeaSPNegativeExtended", func(a *Asm) { a.LeaSP(SP, -0x12345) }, []uint32{1 << 16, 1 << 16, 1 << 31}},
		{"BaseDispX17", func(a *Asm) { a.materializeBaseDisp(X6, X16, 0x12345) }, []uint32{1 << 17, 1 << 17, 1 << 6}},
		{"AddDispZero", func(a *Asm) { a.addDispX16(0) }, nil},
		{"AddDispSmall", func(a *Asm) { a.addDispX16(-16) }, []uint32{1 << 16}},
		{"AddDispLarge", func(a *Asm) { a.addDispX16(0x12345) }, []uint32{1 << 17, 1 << 17, 1 << 16}},
		{"LoadIdxFallback", func(a *Asm) { a.LoadIdx(X6, X8, X9, -16, 8, false, true) }, []uint32{1 << 16, 1 << 16, 1 << 6}},
		{"StoreIdxFallback", func(a *Asm) { a.StoreIdx(X8, X9, X6, -16, 8) }, []uint32{1 << 16, 1 << 16}},
		{"StoreImmZero", func(a *Asm) { a.StoreImmIdx(X8, X9, 0, 0, 8) }, nil},
		{"StoreImmValue", func(a *Asm) { a.StoreImmIdx(X8, X9, 0, 3, 8) }, []uint32{1 << 17}},
		{"StoreImmFallback", func(a *Asm) { a.StoreImmIdx(X8, X9, -16, 3, 8) }, []uint32{1 << 16, 1 << 16, 1 << 17}},
		{"LdrQAddressImmediate", func(a *Asm) { a.LdrQ(31, SP, 257) }, []uint32{1 << 16}},
		{"StrQAddressImmediate", func(a *Asm) { a.StrQ(SP, 257, 31) }, []uint32{1 << 16}},
		{"LdrQAddressX16", func(a *Asm) { a.LdrQ(31, SP, 0x12345) }, []uint32{1 << 16, 1 << 16}},
		{"StrQAddressX17", func(a *Asm) { a.StrQ(X16, 0x12345, 31) }, []uint32{1 << 17, 1 << 17}},
		{"LdrFIdxAddress", func(a *Asm) { a.LdrFIdx(31, X8, X9, -16, true) }, []uint32{1 << 16, 1 << 16}},
		{"StrFIdxAddress", func(a *Asm) { a.StrFIdx(X8, X9, 31, -16, true) }, []uint32{1 << 16, 1 << 16}},
		{"LdrQIdxAddress", func(a *Asm) { a.LdrQIdx(31, X8, X9, -16) }, []uint32{1 << 16, 1 << 16}},
		{"StrQIdxAddress", func(a *Asm) { a.StrQIdx(X8, X9, 31, -16) }, []uint32{1 << 16, 1 << 16}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a Asm
			got := recordGPWrites(t, &a)
			tt.emit(&a)
			if !reflect.DeepEqual(*got, tt.want) {
				t.Fatalf("writes %#x, want %#x", *got, tt.want)
			}
		})
	}
}

func TestGPWritesMaterializedConstants(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, logical := range []bool{false, true} {
			for _, value := range []uint64{0, 1, 0x12345678, 0x123456789abcdef0, 0xff00ff00ff00ff00, ^uint64(0)} {
				for _, wide := range []bool{false, true} {
					var a Asm
					a.DisableCompactMoveImmediate32 = !compact
					a.DisableLogicalMoveImmediate = !logical
					got := recordGPWrites(t, &a)
					if wide {
						a.MovImm64(X6, value)
					} else {
						a.MovImm32(X6, int32(value))
					}
					if len(*got) != a.Len()/4 {
						t.Fatalf("value %#x: %d callbacks for %d instructions", value, len(*got), a.Len()/4)
					}
					for _, mask := range *got {
						if mask != 1<<6 {
							t.Fatalf("value %#x: writes %#x", value, *got)
						}
					}
				}
			}
		}
	}
}

func TestGPWritesSignedScaledLoadsAndIndexedReuse(t *testing.T) {
	for _, size := range []int{1, 2, 4} {
		for _, wide := range []bool{false, true} {
			for _, dst := range []Reg{X6, XZR} {
				var a Asm
				got := recordGPWrites(t, &a)
				if !a.loadDisp(dst, SP, 8, size, true, wide) {
					t.Fatal("valid signed load rejected")
				}
				var want []uint32
				if dst != XZR {
					want = []uint32{1 << 6}
				}
				if !reflect.DeepEqual(*got, want) {
					t.Fatalf("size %d wide %t: writes %#x, want %#x", size, wide, *got, want)
				}
			}
		}
	}
	old := foldIdxDispEnabled
	foldIdxDispEnabled = true
	defer func() { foldIdxDispEnabled = old }()
	var a Asm
	a.DenseIdxDisp, a.ReuseIndexedBase = true, true
	got := recordGPWrites(t, &a)
	a.LoadIdx(X6, X8, X9, 8, 8, false, true)
	a.LoadIdx(X7, X8, X9, 16, 8, false, true)
	want := []uint32{1 << 16, 1 << 6, 1 << 7}
	if !reflect.DeepEqual(*got, want) || a.IndexedBaseReuses != 1 {
		t.Fatalf("reuse: writes %#x, reuses %d", *got, a.IndexedBaseReuses)
	}
}

func TestGPWritesReadOnlyAndOtherBankInstructions(t *testing.T) {
	tests := []struct {
		name string
		emit func(*Asm)
	}{
		{"CmpReg32", func(a *Asm) { a.CmpReg32(X6, X7) }},
		{"CmpReg64", func(a *Asm) { a.CmpReg64(X6, X7) }},
		{"CmpImm32", func(a *Asm) { a.CmpImm32(X6, 3) }},
		{"CmpImm64", func(a *Asm) { a.CmpImm64(X6, 3) }},
		{"CmpImm32LSL12", func(a *Asm) { a.CmpImm32LSL12(X6, 4096) }},
		{"CmpImm64LSL12", func(a *Asm) { a.CmpImm64LSL12(X6, 4096) }},
		{"CmnImm32", func(a *Asm) { a.CmnImm32(X6, 3) }},
		{"CmnImm64", func(a *Asm) { a.CmnImm64(X6, 3) }},
		{"CmnImm32LSL12", func(a *Asm) { a.CmnImm32LSL12(X6, 4096) }},
		{"CmnImm64LSL12", func(a *Asm) { a.CmnImm64LSL12(X6, 4096) }},
		{"CmpSP64", func(a *Asm) { a.CmpSP64(X6) }},
		{"TstImm32", func(a *Asm) { a.TstImm32(X6, 255) }},
		{"TstImm64", func(a *Asm) { a.TstImm64(X6, 255) }},
		{"TstReg32", func(a *Asm) { a.TstReg(X6, X7, true) }},
		{"TstReg64", func(a *Asm) { a.TstReg(X6, X7, false) }},
		{"Store64", func(a *Asm) { a.Store64(X6, SP, 8) }},
		{"Store32", func(a *Asm) { a.Store32(X6, SP, 8) }},
		{"Strb", func(a *Asm) { a.Strb(X6, SP, 8) }},
		{"Strh", func(a *Asm) { a.Strh(X6, SP, 8) }},
		{"Stur64", func(a *Asm) { a.Stur64(X6, SP, -8) }},
		{"Stur32", func(a *Asm) { a.Stur32(X6, SP, -8) }},
		{"StpOffset", func(a *Asm) { a.StpOffset(X6, X7, SP, 8) }},
		{"StrIdx", func(a *Asm) { a.StrIdx(X6, SP, X7, 8) }},
		{"StoreIdxZero", func(a *Asm) { a.StoreIdx(SP, X7, X6, 0, 8) }},
		{"StlrByte", func(a *Asm) { a.Stlr(X6, SP, 1) }},
		{"StlrHalf", func(a *Asm) { a.Stlr(X6, SP, 2) }},
		{"StlrWord", func(a *Asm) { a.Stlr(X6, SP, 4) }},
		{"StlrWide", func(a *Asm) { a.Stlr(X6, SP, 8) }},
		{"DmbIsh", func(a *Asm) { a.DmbIsh() }},
		{"Clrex", func(a *Asm) { a.Clrex() }},
		{"Ret", func(a *Asm) { a.Ret() }},
		{"Br", func(a *Asm) { a.Br(LR) }},
		{"Branch", func(a *Asm) { a.Branch() }},
		{"Bcond", func(a *Asm) { a.Bcond(CondEQ) }},
		{"Cbz32", func(a *Asm) { a.Cbz32(X6) }},
		{"Cbz64", func(a *Asm) { a.Cbz64(X6) }},
		{"Cbnz32", func(a *Asm) { a.Cbnz32(X6) }},
		{"Cbnz64", func(a *Asm) { a.Cbnz64(X6) }},
		{"Nop", func(a *Asm) { a.Nop() }},
		{"Nop4", func(a *Asm) { a.Nop4() }},
		{"Align16", func(a *Asm) { a.Nop(); a.Align16() }},
		{"ScalarFP", func(a *Asm) {
			for _, wide := range []bool{false, true} {
				a.Fadd(31, 6, 7, wide)
				a.Fsub(31, 6, 7, wide)
				a.Fmul(31, 6, 7, wide)
				a.Fdiv(31, 6, 7, wide)
				a.Fsqrt(31, 6, wide)
				a.Fmin(31, 6, 7, wide)
				a.Fmax(31, 6, 7, wide)
				a.FmovReg(31, 6, wide)
				a.FMov(31, 6, wide)
				a.FmovFromGpr(31, X6, wide)
				a.FmovImm(31, 0, wide)
				a.Fcmp(6, 7, wide)
				a.Frint(31, 6, wide, 'n')
				a.Scvtf(31, X6, wide, true)
				a.Ucvtf(31, X6, wide, true)
				a.CvtI2F(31, X6, wide, true)
				a.FcvtS2D(31, 6)
				a.FcvtD2S(31, 6)
				a.LdrLiteralF(31, wide)
				a.FLoadDisp(31, SP, 16, wide)
				a.FStoreDisp(SP, 16, 31, wide)
				a.LdrF(31, SP, 16, wide)
				a.StrF(SP, 16, 31, wide)
				a.LdrFIdx(31, SP, X6, 0, wide)
				a.StrFIdx(SP, X6, 31, 0, wide)
			}
		}},
		{"Vector", func(a *Asm) {
			a.LdrQ(31, SP, 16)
			a.StrQ(SP, 16, 31)
			a.LdrQ(31, SP, -16)
			a.StrQ(SP, -16, 31)
			a.LdrQIdx(31, SP, X6, 0)
			a.StrQIdx(SP, X6, 31, 0)
			a.LdpQ(30, 31, SP, 16)
			a.StpQ(30, 31, SP, 16)
			a.VMovdquLoadDisp(31, SP, 16)
			a.VMovdquStoreDisp(SP, 16, 31)
			a.NeonMov16b(31, 6)
			a.Cnt8b(31, 6)
			a.Addv8b(31, 6)
			a.NeonInsB(31, X6, 0)
			a.NeonInsH(31, X6, 0)
			a.NeonInsS(31, X6, 0)
			a.NeonInsD(31, X6, 0)
			a.NeonDupGprB(31, X6)
			a.NeonDupGprH(31, X6)
			a.NeonDupGprS(31, X6)
			a.NeonDupGprD(31, X6)
			a.NeonFcvtzsSfromS(31, 6)
			a.NeonFcvtzuSfromS(31, 6)
			a.NeonFcvtzsDfromD(31, 6)
			a.NeonFcvtzuDfromD(31, 6)
			a.And16b(31, 6, 7)
			a.Orr16b(31, 6, 7)
			a.Eor16b(31, 6, 7)
			a.NeonAddS(31, 6, 7)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a Asm
			got := recordGPWrites(t, &a)
			tt.emit(&a)
			if len(*got) != 0 {
				t.Fatalf("unexpected GP writes %#x", *got)
			}
		})
	}
}

func TestGPWritesRejectedAndNonEmittingPaths(t *testing.T) {
	rejected := []struct {
		name string
		emit func(*Asm) bool
	}{
		{"Load64Alignment", func(a *Asm) bool { return a.Load64(X6, SP, 1) }},
		{"Load32Range", func(a *Asm) bool { return a.Load32(X6, SP, 1<<16) }},
		{"LdrhAlignment", func(a *Asm) bool { return a.Ldrh(X6, SP, 1) }},
		{"LdrbRange", func(a *Asm) bool { return a.Ldrb(X6, SP, 4096) }},
		{"AndImmZero", func(a *Asm) bool { return a.AndImm64(X6, X7, 0) }},
		{"OrrImmOnes", func(a *Asm) bool { return a.OrrImm64(X6, X7, ^uint64(0)) }},
		{"EorImmZero", func(a *Asm) bool { return a.EorImm32(X6, X7, 0) }},
		{"LogicalPattern", func(a *Asm) bool { return a.AndImm32(SP, X7, 0x12345678) }},
		{"PairSize", func(a *Asm) bool { return a.LoadPairIdx(X6, X7, X8, X9, 16, 2) }},
		{"PairNegative", func(a *Asm) bool { return a.LoadPairIdx(X6, X7, X8, X9, -8, 8) }},
		{"PairAlignment", func(a *Asm) bool { return a.LoadPairIdx(X6, X7, X8, X9, 1, 8) }},
		{"PairRange", func(a *Asm) bool { return a.LoadPairIdx(X6, X7, X8, X9, 512, 8) }},
		{"LoadDispNegative", func(a *Asm) bool { return a.loadDisp(X6, SP, -1, 4, true, true) }},
		{"LoadDispSize", func(a *Asm) bool { return a.loadDisp(X6, SP, 8, 16, true, true) }},
		{"LoadDispAlignment", func(a *Asm) bool { return a.loadDisp(X6, SP, 1, 2, true, true) }},
		{"BaseDispImmediate", func(a *Asm) bool { return a.baseDispImmediate(X6, SP, 0x12345) }},
		{"ScaledSignedRange", func(a *Asm) bool { return a.ldStrScaled(0xB9800000, 2, X6, SP, 1<<16) }},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			var a Asm
			got := recordGPWrites(t, &a)
			if tt.emit(&a) || a.Len() != 0 || len(*got) != 0 {
				t.Fatalf("rejected form emitted %d bytes, writes %#x", a.Len(), *got)
			}
		})
	}
	invalid := []struct {
		name string
		emit func(*Asm)
	}{
		{"LdaxrSize", func(a *Asm) { a.Ldaxr(X6, SP, 3) }},
		{"StlxrSize", func(a *Asm) { a.Stlxr(X6, X7, SP, 3) }},
		{"LdarSize", func(a *Asm) { a.Ldar(X6, SP, 3) }},
		{"StlrSize", func(a *Asm) { a.Stlr(X6, SP, 3) }},
		{"LdrSAlignment", func(a *Asm) { a.LdrS(6, SP, 1) }},
		{"LdrDNegative", func(a *Asm) { a.LdrD(6, SP, -8) }},
		{"LdpQRange", func(a *Asm) { a.LdpQ(6, 7, SP, 1024) }},
		{"StpQAlignment", func(a *Asm) { a.StpQ(6, 7, SP, 1) }},
		{"FrintMode", func(a *Asm) { a.Frint(6, 7, true, '?') }},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			var a Asm
			got := recordGPWrites(t, &a)
			defer func() {
				if recover() == nil {
					t.Fatal("invalid form did not panic")
				}
				if a.Len() != 0 || len(*got) != 0 {
					t.Fatalf("invalid form emitted %d bytes, writes %#x", a.Len(), *got)
				}
			}()
			tt.emit(&a)
		})
	}
	var a Asm
	branch := a.Branch()
	cond := a.Bcond(CondEQ)
	adr := a.Adr(X6)
	literal := a.LdrLiteralF(6, true)
	mov := a.Len()
	a.Movz64(X6, 0, 0)
	a.Movk64(X6, 0, 1)
	got := recordGPWrites(t, &a)
	a.PatchBranch26(branch, 4)
	a.PatchBranch19(cond, 8)
	a.PatchAdr(adr, 12)
	a.PatchLiteral19(literal, 16)
	a.PatchMovImm(mov, 0x12345678)
	a.PatchU32(branch, 0)
	a.Grow(100)
	a.NeonPshufS(6, 6, 0)
	if len(*got) != 0 {
		t.Fatalf("patch/non-emitting paths reported writes %#x", *got)
	}
}

func TestGPWritesObserverScopesAndTransferIndependence(t *testing.T) {
	var a Asm
	var outer, inner []uint32
	var transfers []regalloccheck.Effect
	if old := a.ObserveGPWrites(func(mask uint32) { outer = append(outer, mask) }); old != nil {
		t.Fatal("initial observer is not nil")
	}
	a.ObserveRegalloc(func(e regalloccheck.Effect) { transfers = append(transfers, e) })
	a.MovReg64(X6, X7)
	previous := a.ObserveGPWrites(func(mask uint32) { inner = append(inner, mask) })
	a.MovReg32(X8, X9)
	a.ObserveGPWrites(previous)
	a.Add64(X10, X11, X12)
	// Physical arithmetic writes must not be added to the transfer model.
	if len(transfers) != 2 || transfers[0].Kind != regalloccheck.Copy || transfers[1].Kind != regalloccheck.Copy {
		t.Fatalf("changed transfers: %+v", transfers)
	}
	if !reflect.DeepEqual(outer, []uint32{1 << 6, 1 << 10}) || !reflect.DeepEqual(inner, []uint32{1 << 8}) {
		t.Fatalf("outer %#x, inner %#x", outer, inner)
	}
	old := a.ObserveGPWrites(nil)
	a.MovReg64(X12, X13)
	if len(transfers) != 3 || len(outer) != 2 {
		t.Fatal("disabling GP observer changed transfer observer")
	}
	a.ObserveGPWrites(old)
	transferOld := a.ObserveRegalloc(nil)
	a.MovReg64(X14, X15)
	if len(transfers) != 3 || len(outer) != 3 || outer[2] != 1<<14 {
		t.Fatal("disabling transfer observer changed GP observer")
	}
	a.ObserveRegalloc(transferOld)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("missing test panic")
			}
		}()
		old := a.ObserveGPWrites(nil)
		defer a.ObserveGPWrites(old)
		panic("scope unwind")
	}()
	a.Add64(X6, X7, X8)
	if len(outer) != 4 || outer[3] != 1<<6 {
		t.Fatalf("observer not restored after panic: %#x", outer)
	}
}
