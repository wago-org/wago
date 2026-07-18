package riscv64

import "testing"

func TestScalarFloatingPointEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want uint32
	}{
		{"fadd.s f2,f0,f1", func(a *Asm) { must(t, a.Fadd(F2, F0, F1, false, RoundNearestEven)) }, 0x00100153},
		{"fadd.d f2,f0,f1", func(a *Asm) { must(t, a.Fadd(F2, F0, F1, true, RoundNearestEven)) }, 0x02100153},
		{"fsub.s f2,f0,f1", func(a *Asm) { must(t, a.Fsub(F2, F0, F1, false, RoundNearestEven)) }, 0x08100153},
		{"fmul.s f2,f0,f1", func(a *Asm) { must(t, a.Fmul(F2, F0, F1, false, RoundNearestEven)) }, 0x10100153},
		{"fdiv.s f2,f0,f1", func(a *Asm) { must(t, a.Fdiv(F2, F0, F1, false, RoundNearestEven)) }, 0x18100153},
		{"fsqrt.s f1,f0", func(a *Asm) { must(t, a.Fsqrt(F1, F0, false, RoundNearestEven)) }, 0x580000d3},
		{"fmin.s f2,f0,f1", func(a *Asm) { a.Fmin(F2, F0, F1, false) }, 0x28100153},
		{"fmax.s f2,f0,f1", func(a *Asm) { a.Fmax(F2, F0, F1, false) }, 0x28101153},
		{"fsgnj.s f2,f0,f1", func(a *Asm) { a.Fsgnj(F2, F0, F1, false) }, 0x20100153},
		{"fsgnjn.s f2,f0,f1", func(a *Asm) { a.Fsgnjn(F2, F0, F1, false) }, 0x20101153},
		{"fsgnjx.s f2,f0,f1", func(a *Asm) { a.Fsgnjx(F2, F0, F1, false) }, 0x20102153},
		{"fmv.s f1,f0", func(a *Asm) { a.FmovReg(F1, F0, false) }, 0x200000d3},
		{"fabs.s f1,f0", func(a *Asm) { a.Fabs(F1, F0, false) }, 0x200020d3},
		{"fneg.s f1,f0", func(a *Asm) { a.Fneg(F1, F0, false) }, 0x200010d3},
		{"feq.s x7,f0,f1", func(a *Asm) { a.Feq(X7, F0, F1, false) }, 0xa01023d3},
		{"flt.s x7,f0,f1", func(a *Asm) { a.Flt(X7, F0, F1, false) }, 0xa01013d3},
		{"fle.s x7,f0,f1", func(a *Asm) { a.Fle(X7, F0, F1, false) }, 0xa01003d3},
		{"fclass.s x5,f0", func(a *Asm) { a.Fclass(X5, F0, false) }, 0xe00012d3},
		{"fmv.x.w x5,f0", func(a *Asm) { a.FmvToGPR(X5, F0, false) }, 0xe00002d3},
		{"fmv.w.x f0,x5", func(a *Asm) { a.FmvFromGPR(F0, X5, false) }, 0xf0028053},
		{"fmv.x.d x5,f0", func(a *Asm) { a.FmvToGPR(X5, F0, true) }, 0xe20002d3},
		{"fmv.d.x f0,x5", func(a *Asm) { a.FmvFromGPR(F0, X5, true) }, 0xf2028053},
		{"fcvt.w.s x5,f0", func(a *Asm) { must(t, a.FcvtFloatToInt(X5, F0, false, false, false, RoundTowardZero)) }, 0xc00012d3},
		{"fcvt.l.s x5,f0", func(a *Asm) { must(t, a.FcvtFloatToInt(X5, F0, false, false, true, RoundTowardZero)) }, 0xc02012d3},
		{"fcvt.wu.d x5,f0", func(a *Asm) { must(t, a.FcvtFloatToInt(X5, F0, true, true, false, RoundTowardZero)) }, 0xc21012d3},
		{"fcvt.s.w f0,x5", func(a *Asm) { must(t, a.FcvtIntToFloat(F0, X5, false, false, false, RoundNearestEven)) }, 0xd0028053},
		{"fcvt.d.lu f0,x5", func(a *Asm) { must(t, a.FcvtIntToFloat(F0, X5, true, true, true, RoundNearestEven)) }, 0xd2328053},
		{"fcvt.d.s f1,f0", func(a *Asm) { must(t, a.FcvtS2D(F1, F0, RoundNearestEven)) }, 0x420000d3},
		{"fcvt.s.d f1,f0", func(a *Asm) { must(t, a.FcvtD2S(F1, F0, RoundNearestEven)) }, 0x401000d3},
		{"fmadd.s f4,f1,f2,f3", func(a *Asm) { must(t, a.Fmadd(F4, F1, F2, F3, false, RoundNearestEven)) }, 0x18208243},
		{"fmsub.s f4,f1,f2,f3", func(a *Asm) { must(t, a.Fmsub(F4, F1, F2, F3, false, RoundNearestEven)) }, 0x18208247},
		{"fnmsub.s f4,f1,f2,f3", func(a *Asm) { must(t, a.Fnmsub(F4, F1, F2, F3, false, RoundNearestEven)) }, 0x1820824b},
		{"fnmadd.s f4,f1,f2,f3", func(a *Asm) { must(t, a.Fnmadd(F4, F1, F2, F3, false, RoundNearestEven)) }, 0x1820824f},
		{"fmadd.d f4,f1,f2,f3", func(a *Asm) { must(t, a.Fmadd(F4, F1, F2, F3, true, RoundNearestEven)) }, 0x1a208243},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			if got := word(&a); got != tc.want {
				t.Fatalf("got %#08x, want %#08x", got, tc.want)
			}
		})
	}
}

func TestFloatingPointLoadStoreEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want uint32
	}{
		{"flw f0,4(x5)", func(a *Asm) { must(t, a.Flw(F0, X5, 4)) }, 0x0042a007},
		{"fld f0,4(x5)", func(a *Asm) { must(t, a.Fld(F0, X5, 4)) }, 0x0042b007},
		{"fsw f0,4(x5)", func(a *Asm) { must(t, a.Fsw(F0, X5, 4)) }, 0x0002a227},
		{"fsd f0,4(x5)", func(a *Asm) { must(t, a.Fsd(F0, X5, 4)) }, 0x0002b227},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			if got := word(&a); got != tc.want {
				t.Fatalf("got %#08x, want %#08x", got, tc.want)
			}
		})
	}
}

func TestAtomicEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want uint32
	}{
		{"lr.w x6,(x5)", func(a *Asm) { a.Lr32(X6, X5, OrderRelaxed) }, 0x1002a32f},
		{"lr.d x6,(x5)", func(a *Asm) { a.Lr64(X6, X5, OrderRelaxed) }, 0x1002b32f},
		{"sc.w x7,x5,(x6)", func(a *Asm) { a.Sc32(X7, X6, X5, OrderRelaxed) }, 0x185323af},
		{"sc.d x7,x5,(x6)", func(a *Asm) { a.Sc64(X7, X6, X5, OrderRelaxed) }, 0x185333af},
		{"amoswap.w x7,x5,(x6)", func(a *Asm) { a.AmoSwap32(X7, X6, X5, OrderRelaxed) }, 0x085323af},
		{"amoadd.d x7,x5,(x6)", func(a *Asm) { a.AmoAdd64(X7, X6, X5, OrderRelaxed) }, 0x005333af},
		{"amoand.w x7,x5,(x6)", func(a *Asm) { a.AmoAnd32(X7, X6, X5, OrderRelaxed) }, 0x605323af},
		{"amoor.d x7,x5,(x6)", func(a *Asm) { a.AmoOr64(X7, X6, X5, OrderRelaxed) }, 0x405333af},
		{"amoxor.w x7,x5,(x6)", func(a *Asm) { a.AmoXor32(X7, X6, X5, OrderRelaxed) }, 0x205323af},
		{"amomin.d x7,x5,(x6)", func(a *Asm) { a.AmoMin64(X7, X6, X5, OrderRelaxed) }, 0x805333af},
		{"amomax.w x7,x5,(x6)", func(a *Asm) { a.AmoMax32(X7, X6, X5, OrderRelaxed) }, 0xa05323af},
		{"amominu.d x7,x5,(x6)", func(a *Asm) { a.AmoMinu64(X7, X6, X5, OrderRelaxed) }, 0xc05333af},
		{"amomaxu.w.aqrl x7,x5,(x6)", func(a *Asm) { a.AmoMaxu32(X7, X6, X5, OrderAcquireRelease) }, 0xe65323af},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			if got := word(&a); got != tc.want {
				t.Fatalf("got %#08x, want %#08x", got, tc.want)
			}
		})
	}
}

func TestCSREncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want uint32
	}{
		{"csrrs x5,cycle,x0", func(a *Asm) { a.Csrrs(X5, 0xc00, Zero) }, 0xc00022f3},
		{"csrrw x5,fflags,x6", func(a *Asm) { a.Csrrw(X5, 0x001, X6) }, 0x001312f3},
		{"csrrc x5,frm,x6", func(a *Asm) { a.Csrrc(X5, 0x002, X6) }, 0x002332f3},
		{"csrrwi x5,fcsr,7", func(a *Asm) { must(t, a.Csrrwi(X5, 0x003, 7)) }, 0x0033d2f3},
		{"csrrsi x5,fcsr,7", func(a *Asm) { must(t, a.Csrrsi(X5, 0x003, 7)) }, 0x0033e2f3},
		{"csrrci x5,fcsr,7", func(a *Asm) { must(t, a.Csrrci(X5, 0x003, 7)) }, 0x0033f2f3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			if got := word(&a); got != tc.want {
				t.Fatalf("got %#08x, want %#08x", got, tc.want)
			}
		})
	}
}

func TestScalarExtensionRangeFailures(t *testing.T) {
	checks := []func(*Asm) bool{
		func(a *Asm) bool { return a.Fadd(F0, F1, F2, false, 5) },
		func(a *Asm) bool { return a.Fsqrt(F0, F1, true, 6) },
		func(a *Asm) bool { return a.Fmadd(F0, F1, F2, F3, false, 5) },
		func(a *Asm) bool { return a.FcvtFloatToInt(X0, F0, false, false, false, 6) },
		func(a *Asm) bool { return a.FcvtIntToFloat(F0, X0, true, true, true, 5) },
		func(a *Asm) bool { return a.Flw(F0, X1, 2048) },
		func(a *Asm) bool { return a.Fsd(F0, X1, -2049) },
		func(a *Asm) bool { return a.Csrrwi(X0, 0, 32) },
	}
	for i, check := range checks {
		var a Asm
		if check(&a) {
			t.Fatalf("check %d accepted invalid input", i)
		}
		if len(a.B) != 0 {
			t.Fatalf("check %d emitted %d bytes", i, len(a.B))
		}
	}
}

func TestFloatingPointConvenienceWrappers(t *testing.T) {
	var a Asm
	must(t, a.Fadd(F0, F1, F2, false, RoundDynamic))
	must(t, a.Fsub(F0, F1, F2, true, RoundUp))
	must(t, a.Fmul(F0, F1, F2, false, RoundDown))
	must(t, a.Fdiv(F0, F1, F2, true, RoundTowardZero))
	must(t, a.Fsqrt(F0, F1, false, RoundNearestMax))
	a.Fmin(F0, F1, F2, true)
	a.Fmax(F0, F1, F2, false)
	a.FmovReg(F0, F1, true)
	a.Fabs(F0, F1, false)
	a.Fneg(F0, F1, true)
	a.Fle(X0, F1, F2, true)
	a.Flt(X0, F1, F2, false)
	a.Feq(X0, F1, F2, true)
	a.Fclass(X0, F1, false)
	a.FmvToGPR(X0, F1, true)
	a.FmvFromGPR(F0, X1, false)
	must(t, a.FcvtFloatToInt(X0, F1, true, true, true, RoundTowardZero))
	must(t, a.FcvtIntToFloat(F0, X1, false, true, true, RoundNearestEven))
	must(t, a.FcvtS2D(F0, F1, RoundNearestEven))
	must(t, a.FcvtD2S(F0, F1, RoundNearestEven))
	must(t, a.FLoad(F0, X1, 0, false))
	must(t, a.FLoad(F0, X1, 8, true))
	must(t, a.FStore(F0, X1, 0, false))
	must(t, a.FStore(F0, X1, 8, true))
	if len(a.B) != 24*4 {
		t.Fatalf("wrappers emitted %d bytes, want %d", len(a.B), 24*4)
	}
}
