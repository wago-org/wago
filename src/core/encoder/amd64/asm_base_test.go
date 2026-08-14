package amd64

import (
	"bytes"
	"testing"
	"unsafe"
)

func TestScalarAndFrameEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want []byte
	}{
		{"sub32", func(a *Asm) { a.Sub32(RAX, RCX) }, []byte{0x29, 0xc8}},
		{"and32", func(a *Asm) { a.And32(RAX, RCX) }, []byte{0x21, 0xc8}},
		{"or32", func(a *Asm) { a.Or32(RAX, RCX) }, []byte{0x09, 0xc8}},
		{"xor32", func(a *Asm) { a.Xor32(RAX, RCX) }, []byte{0x31, 0xc8}},
		{"xor8 low", func(a *Asm) { a.AluRR8(0x30, RAX, RCX) }, []byte{0x30, 0xc8}},
		{"xor8 rex-low", func(a *Asm) { a.AluRR8(0x30, RSP, RDI) }, []byte{0x40, 0x30, 0xfc}},
		{"xor8 extended", func(a *Asm) { a.AluRR8(0x30, R8, R9) }, []byte{0x45, 0x30, 0xc8}},
		{"setcc al", func(a *Asm) { a.SetccAL(CondE) }, []byte{0x0f, 0x94, 0xc0, 0x0f, 0xb6, 0xc0}},
		{"leave", func(a *Asm) { a.Leave() }, []byte{0xc9}},
		{"prologue", func(a *Asm) { a.Prologue() }, []byte{0x55, 0x48, 0x89, 0xe5}},
		{"store rsp32", func(a *Asm) { a.StoreRsp32(4, RAX) }, []byte{0x89, 0x44, 0x24, 4}},
		{"load rsp32", func(a *Asm) { a.LoadRsp32(RCX, 8) }, []byte{0x8b, 0x4c, 0x24, 8}},
		{"store rsp64", func(a *Asm) { a.StoreRsp64(12, R8) }, []byte{0x4c, 0x89, 0x44, 0x24, 12}},
		{"load rsp64", func(a *Asm) { a.LoadRsp64(R9, 16) }, []byte{0x4c, 0x8b, 0x4c, 0x24, 16}},
		{"mov from rsp", func(a *Asm) { a.MovFromRsp(RAX) }, []byte{0x48, 0x89, 0xe0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			if !bytes.Equal(a.B, tc.want) {
				t.Fatalf("encoding = %x, want %x", a.B, tc.want)
			}
		})
	}
}

func TestScalarFloatEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want []byte
	}{
		{"mulss", func(a *Asm) { a.FMul(0, 1, false) }, []byte{0xf3, 0x0f, 0x59, 0xc1}},
		{"divsd", func(a *Asm) { a.FDiv(0, 1, true) }, []byte{0xf2, 0x0f, 0x5e, 0xc1}},
		{"minss", func(a *Asm) { a.FMin(0, 1, false) }, []byte{0xf3, 0x0f, 0x5d, 0xc1}},
		{"maxsd", func(a *Asm) { a.FMax(0, 1, true) }, []byte{0xf2, 0x0f, 0x5f, 0xc1}},
		{"sqrtss", func(a *Asm) { a.FSqrt(0, 1, false) }, []byte{0xf3, 0x0f, 0x51, 0xc1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			if !bytes.Equal(a.B, tc.want) {
				t.Fatalf("encoding = %x, want %x", a.B, tc.want)
			}
		})
	}
}

// TestIntegerMemoryAndControlEncodings exercises the forms the compiler uses
// for register allocation, folded memory operands, and branch fixups. Keeping
// these as byte-level goldens makes prefix and ModRM/SIB regressions visible.
func TestIntegerMemoryAndControlEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want []byte
	}{
		{"push extended", func(a *Asm) { a.Push(R9) }, []byte{0x41, 0x51}},
		{"pop", func(a *Asm) { a.Pop(RDI) }, []byte{0x5f}},
		{"mov immediate 32", func(a *Asm) { a.MovImm32(R8, -2) }, []byte{0x41, 0xb8, 0xfe, 0xff, 0xff, 0xff}},
		{"mov register 32", func(a *Asm) { a.MovRegReg32(R9, R8) }, []byte{0x45, 0x89, 0xc1}},
		{"lzcnt 64", func(a *Asm) { a.Lzcnt(R9, R8, true) }, []byte{0xf3, 0x4d, 0x0f, 0xbd, 0xc8}},
		{"tzcnt 32", func(a *Asm) { a.Tzcnt(RAX, RCX, false) }, []byte{0xf3, 0x0f, 0xbc, 0xc1}},
		{"popcnt 64", func(a *Asm) { a.Popcnt(RAX, R9, true) }, []byte{0xf3, 0x49, 0x0f, 0xb8, 0xc1}},
		{"mov register 64", func(a *Asm) { a.MovReg64(R9, R8) }, []byte{0x4d, 0x89, 0xc1}},
		{"xchg 64", func(a *Asm) { a.Xchg64(R8, R9) }, []byte{0x4d, 0x87, 0xc1}},
		{"lock xadd 32", func(a *Asm) { a.LockXaddIdx32(R10, R11, R9, 8) }, []byte{0xf0, 0x47, 0x0f, 0xc1, 0x4c, 0x1a, 0x08}},
		{"xchg memory 32", func(a *Asm) { a.XchgIdx(R10, R11, R9, 8, 4) }, []byte{0x47, 0x87, 0x4c, 0x1a, 0x08}},
		{"mfence", func(a *Asm) { a.Mfence() }, []byte{0x0f, 0xae, 0xf0}},
		{"lock xadd byte", func(a *Asm) { a.LockXaddIdx(R10, R11, R9, 8, 1) }, []byte{0xf0, 0x47, 0x0f, 0xc0, 0x4c, 0x1a, 0x08}},
		{"lock xadd 64", func(a *Asm) { a.LockXaddIdx(R10, R11, R9, 8, 8) }, []byte{0xf0, 0x4f, 0x0f, 0xc1, 0x4c, 0x1a, 0x08}},
		{"movzx r9d,r10b", func(a *Asm) { a.Movzx8(R9, R10, false) }, []byte{0x45, 0x0f, 0xb6, 0xca}},
		{"lock cmpxchg 64", func(a *Asm) { a.LockCmpxchgIdx(R10, R11, R9, 8, 8) }, []byte{0xf0, 0x4f, 0x0f, 0xb1, 0x4c, 0x1a, 0x08}},
		{"movsxd", func(a *Asm) { a.Movsxd(R8, R9) }, []byte{0x4d, 0x63, 0xc1}},
		{"movsx byte rex", func(a *Asm) { a.Movsx8(RAX, RSP, false) }, []byte{0x40, 0x0f, 0xbe, 0xc4}},
		{"movsx word", func(a *Asm) { a.Movsx16(R8, R9, true) }, []byte{0x4d, 0x0f, 0xbf, 0xc1}},
		{"load 32 sib", func(a *Asm) { a.Load32(R8, RSP, 4) }, []byte{0x44, 0x8b, 0x44, 0x24, 4}},
		{"store 64 sib", func(a *Asm) { a.Store64(R12, 8, R9) }, []byte{0x4d, 0x89, 0x4c, 0x24, 8}},
		{"store immediate", func(a *Asm) { a.StoreImm32Mem(R8, 12, -1) }, []byte{0x41, 0xc7, 0x40, 12, 0xff, 0xff, 0xff, 0xff}},
		{"store indexed byte", func(a *Asm) { a.StoreImmIdx(R8, R9, 4, 0xab, 1) }, []byte{0x43, 0xc6, 0x44, 0x08, 4, 0xab}},
		{"alu immediate byte", func(a *Asm) { a.AluRI(5, R8, -1, true) }, []byte{0x49, 0x83, 0xe8, 0xff}},
		{"alu immediate long", func(a *Asm) { a.AluRI(0, RAX, 0x1234, false) }, []byte{0x81, 0xc0, 0x34, 0x12, 0, 0}},
		{"imul three operand", func(a *Asm) { a.ImulRRI(R8, R9, 7, true) }, []byte{0x4d, 0x6b, 0xc1, 7}},
		{"shift cl", func(a *Asm) { a.ShiftCL(4, R8, true) }, []byte{0x49, 0xd3, 0xe0}},
		{"mov immediate 64", func(a *Asm) { a.MovImm64(R8, 0x1122334455667788) }, []byte{0x49, 0xb8, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}},
		{"call memory", func(a *Asm) { a.CallMem(R12, 16) }, []byte{0x41, 0xff, 0x54, 0x24, 16}},
		{"call register", func(a *Asm) { a.CallReg(R9) }, []byte{0x41, 0xff, 0xd1}},
		{"lea scaled 32", func(a *Asm) { a.LeaScaledW(R8, R9, R10, 2, -4, false) }, []byte{0x47, 0x8d, 0x44, 0x91, 0xfc}},
		{"load indexed signed", func(a *Asm) { a.LoadIdx(R8, R9, R10, 4, 1, true, true) }, []byte{0x4f, 0x0f, 0xbe, 0x44, 0x11, 4}},
		{"store indexed byte rex", func(a *Asm) { a.StoreIdx(R8, R9, RSP, 0, 1) }, []byte{0x43, 0x88, 0x24, 0x08}},
		{"idiv 64", func(a *Asm) { a.Idiv(R9, true) }, []byte{0x49, 0xf7, 0xf9}},
		{"multiply high", func(a *Asm) { a.IMulHigh(R8, false) }, []byte{0x41, 0xf7, 0xe8}},
		{"conditional move", func(a *Asm) { a.Cmovcc(CondGE, R8, R9, true) }, []byte{0x4d, 0x0f, 0x4d, 0xc1}},
		{"setcc register", func(a *Asm) { a.SetccReg(CondNE, R8) }, []byte{0x41, 0x0f, 0x95, 0xc0, 0x45, 0x0f, 0xb6, 0xc0}},
		{"setcc low byte", func(a *Asm) { a.SetccReg8(CondNE, R8) }, []byte{0x41, 0x0f, 0x95, 0xc0}},
		{"setcc low byte bare rex", func(a *Asm) { a.SetccReg8(CondNE, RSI) }, []byte{0x40, 0x0f, 0x95, 0xc6}},
		{"jump register", func(a *Asm) { a.JmpReg(R9) }, []byte{0x41, 0xff, 0xe1}},
		{"neg", func(a *Asm) { a.Neg(R8, true) }, []byte{0x49, 0xf7, 0xd8}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			if !bytes.Equal(a.B, tc.want) {
				t.Fatalf("encoding = %x, want %x", a.B, tc.want)
			}
		})
	}
}

func TestAssemblerPatchingAndAlignment(t *testing.T) {
	var a Asm
	a.Grow(32)
	if cap(a.B) < 32 {
		t.Fatalf("capacity = %d, want at least 32", cap(a.B))
	}
	call := a.CallRel32()
	a.PatchRel32(call, 12)
	if got, want := a.B, []byte{0xe8, 7, 0, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("patched call = %x, want %x", got, want)
	}
	a.JccPlaceholder(CondE)
	a.PatchRel32(7, a.Len())
	if got, want := a.B[5:], []byte{0x0f, 0x84, 0, 0, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("patched conditional jump = %x, want %x", got, want)
	}
	a.Align16()
	if a.Len()%16 != 0 {
		t.Fatalf("aligned length = %d", a.Len())
	}
	var ordinary, late Asm
	ordinary.B = make([]byte, 9)
	ordinary.AlignLoop()
	if ordinary.Len() != 16 {
		t.Fatalf("ordinary loop-aligned length = %d, want 16", ordinary.Len())
	}
	late.B = make([]byte, 25)
	late.AlignLoop()
	if late.Len() != 40 {
		t.Fatalf("late loop-aligned length = %d, want 40", late.Len())
	}
}

func TestRel32SiteCounting(t *testing.T) {
	a := Asm{Rel32SiteLimit: 1}
	first := a.JmpPlaceholder()
	a.PatchRel32(first, a.Len())
	a.JmpBack(0)
	if got, want := a.Rel32Count, 2; got != want {
		t.Fatalf("rel32 site count = %d, want %d", got, want)
	}
	if got, want := len(a.Rel32Sites), 1; got != want {
		t.Fatalf("retained rel32 sites = %d, want %d", got, want)
	}
	if !a.Rel32Overflow {
		t.Fatal("bounded rel32 recorder did not report overflow")
	}
	if got := a.Rel32Sites[0]; int(got.At) != first || got.Target() != 5 || got.Kind() != Rel32Jmp {
		t.Fatalf("first rel32 site = %+v, want at=%d target=5", got, first)
	}
	if got, want := unsafe.Sizeof(Rel32Site{}), uintptr(8); got != want {
		t.Fatalf("rel32 site size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(Asm{}), uintptr(80); got != want {
		t.Fatalf("asm size = %d, want %d", got, want)
	}
}

func TestRel32SiteRewrite(t *testing.T) {
	a := Asm{Rel32SiteLimit: 4}
	one := a.JmpPlaceholder()
	a.PatchRel32(one, 20)
	two := a.JmpPlaceholder()
	a.PatchRel32(two, 30)
	a.RetargetRel32(one, 40)
	a.ForgetRel32(two)
	if got := a.Rel32Sites; len(got) != 1 || int(got[0].At) != one || got[0].Target() != 40 || got[0].Kind() != Rel32Jmp {
		t.Fatalf("rewritten sites = %+v", got)
	}
	a.Rel32Sites[0].SetShort(true)
	if !a.Rel32Sites[0].Short() || a.Rel32Sites[0].Target() != 40 || a.Rel32Sites[0].Kind() != Rel32Jmp {
		t.Fatalf("short choice corrupted packed site: %+v", a.Rel32Sites[0])
	}
	a.RetargetRel32(one, int(rel32TargetMask)+1)
	if !a.Rel32Overflow {
		t.Fatal("out-of-range packed target did not force identity fallback")
	}
}

func TestBindRel32TailSeparatesCodeAndScratch(t *testing.T) {
	a := Asm{B: make([]byte, 0, 64), Rel32SiteLimit: 2}
	if !a.BindRel32Tail(2) {
		t.Fatal("bind rel32 tail failed")
	}
	if got, want := cap(a.B), 48; got != want {
		t.Fatalf("code capacity = %d, want %d", got, want)
	}
	if got, want := cap(a.Rel32Sites), 2; got != want {
		t.Fatalf("site capacity = %d, want %d", got, want)
	}
	one := a.JmpPlaceholder()
	a.PatchRel32(one, a.Len())
	two := a.JccPlaceholder(CondE)
	a.PatchRel32(two, a.Len())
	a.B = append(a.B, make([]byte, cap(a.B)-len(a.B))...)
	if got := a.Rel32Sites; len(got) != 2 || got[0].Kind() != Rel32Jmp || got[1].Kind() != Rel32Jcc {
		t.Fatalf("tail-backed sites corrupted by code writes: %+v", got)
	}
}

func TestResetRel32RecorderUsesInlineFallback(t *testing.T) {
	a := Asm{}
	a.ResetRel32Recorder(4)
	if got, want := cap(a.Rel32Sites), 1; got != want {
		t.Fatalf("inline site capacity = %d, want %d", got, want)
	}
	a.B = append(a.B, 0xe9)
	site := a.Len()
	a.B = append(a.B, 0, 0, 0, 0)
	a.PatchRel32(site, a.Len())
	if got := len(a.Rel32Sites); got != 1 {
		t.Fatalf("inline site count = %d, want 1", got)
	}
	a.ResetRel32Recorder(0)
	if a.Rel32Sites != nil {
		t.Fatalf("disabled recorder retained inline storage")
	}
}

func TestRemainingIntegerInstructionForms(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want []byte
	}{
		{"store32", func(a *Asm) { a.Store32(R8, 4, R9) }, []byte{0x45, 0x89, 0x48, 4}},
		{"load64", func(a *Asm) { a.Load64(R8, R9, 4) }, []byte{0x4d, 0x8b, 0x41, 4}},
		{"add32", func(a *Asm) { a.Add32(R8, R9) }, []byte{0x45, 0x01, 0xc8}},
		{"cmp32", func(a *Asm) { a.Cmp32(R8, R9) }, []byte{0x45, 0x39, 0xc8}},
		{"imul", func(a *Asm) { a.IMul(R8, R9, true) }, []byte{0x4d, 0x0f, 0xaf, 0xc1}},
		{"test self", func(a *Asm) { a.TestSelf(R8, true) }, []byte{0x4d, 0x85, 0xc0}},
		{"test reg", func(a *Asm) { a.TestReg(R8, R9, true) }, []byte{0x4d, 0x85, 0xc8}},
		{"test imm", func(a *Asm) { a.TestImm(R8, 0x80808080, false) }, []byte{0x41, 0xf7, 0xc0, 0x80, 0x80, 0x80, 0x80}},
		{"return", func(a *Asm) { a.Ret() }, []byte{0xc3}},
		{"sub rsp", func(a *Asm) { a.SubRsp(16) }, []byte{0x48, 0x81, 0xec, 16, 0, 0, 0}},
		{"alu rr", func(a *Asm) { a.AluRR(0x01, R8, R9, true) }, []byte{0x4d, 0x01, 0xc8}},
		{"alu rm", func(a *Asm) { a.AluRM(0x03, R8, R9, 4, true) }, []byte{0x4d, 0x03, 0x41, 4}},
		{"alu indexed", func(a *Asm) { a.AluIdx(0x03, R8, R9, R10, 4, true) }, []byte{0x4f, 0x03, 0x44, 0x11, 4}},
		{"imul memory", func(a *Asm) { a.ImulRM(R8, R9, 4, true) }, []byte{0x4d, 0x0f, 0xaf, 0x41, 4}},
		{"imul immediate", func(a *Asm) { a.ImulRI(R8, 0x1234, true) }, []byte{0x4d, 0x69, 0xc0, 0x34, 0x12, 0, 0}},
		{"shift immediate", func(a *Asm) { a.ShiftImm(4, R8, 3, true) }, []byte{0x49, 0xc1, 0xe0, 3}},
		{"add rsp", func(a *Asm) { a.AddRsp(16) }, []byte{0x48, 0x81, 0xc4, 16, 0, 0, 0}},
		{"lea rsp", func(a *Asm) { a.LeaRsp(R8, 4) }, []byte{0x4c, 0x8d, 0x44, 0x24, 4}},
		{"lea scaled", func(a *Asm) { a.LeaScaled(R8, R9, R10, 1, 4) }, []byte{0x4f, 0x8d, 0x44, 0x51, 4}},
		{"lea displacement", func(a *Asm) { a.LeaDisp(R8, R9, 4) }, []byte{0x4d, 0x8d, 0x41, 4}},
		{"add64", func(a *Asm) { a.Add64(R8, R9) }, []byte{0x4d, 0x01, 0xc8}},
		{"cmp64", func(a *Asm) { a.Cmp64(R8, R9) }, []byte{0x4d, 0x39, 0xc8}},
		{"string operations", func(a *Asm) { a.RepMovsb(); a.RepStosb(); a.Std(); a.Cld() }, []byte{0xf3, 0xa4, 0xf3, 0xaa, 0xfd, 0xfc}},
		{"cdq", func(a *Asm) { a.Cdq(true) }, []byte{0x48, 0x99}},
		{"unsigned divide", func(a *Asm) { a.Div(R8, true) }, []byte{0x49, 0xf7, 0xf0}},
		{"unsigned multiply", func(a *Asm) { a.Mul(R8, true) }, []byte{0x49, 0xf7, 0xe0}},
		{"xor self", func(a *Asm) { a.XorSelf32(R8) }, []byte{0x45, 0x31, 0xc0}},
		{"jump back", func(a *Asm) { a.JmpBack(0) }, []byte{0xe9, 0xfb, 0xff, 0xff, 0xff}},
		{"lea rip placeholder", func(a *Asm) { a.LeaRipPlaceholder(R8) }, []byte{0x4c, 0x8d, 0x05, 0, 0, 0, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			if !bytes.Equal(a.B, tc.want) {
				t.Fatalf("encoding = %x, want %x", a.B, tc.want)
			}
		})
	}
}

func TestAdditionalVEXAndScalarFloatWrappers(t *testing.T) {
	var a Asm
	emit := []func(){
		func() { a.VPaddw(RAX, RCX, RDX) }, func() { a.VPaddd(RAX, RCX, RDX) }, func() { a.VPaddq(RAX, RCX, RDX) },
		func() { a.VPsubb(RAX, RCX, RDX) }, func() { a.VPsubw(RAX, RCX, RDX) }, func() { a.VPsubd(RAX, RCX, RDX) }, func() { a.VPsubq(RAX, RCX, RDX) },
		func() { a.VPand(RAX, RCX, RDX) }, func() { a.VPandn(RAX, RCX, RDX) }, func() { a.VPor(RAX, RCX, RDX) },
		func() { a.VPcmpeqb(RAX, RCX, RDX) }, func() { a.VPcmpeqw(RAX, RCX, RDX) }, func() { a.VPcmpeqd(RAX, RCX, RDX) },
		func() { a.VPcmpgtb(RAX, RCX, RDX) }, func() { a.VPcmpgtw(RAX, RCX, RDX) }, func() { a.VPcmpgtd(RAX, RCX, RDX) }, func() { a.VPcmpgtq(RAX, RCX, RDX) },
		func() { a.VMovmskps(RAX, RCX) }, func() { a.VMovmskpd(RAX, RCX) }, func() { a.VPacksswb(RAX, RCX, RDX) },
		func() { a.VPsllwImm(RAX, RCX, 3) }, func() { a.VPsrldImm(RAX, RCX, 3) }, func() { a.VPsrlqImm(RAX, RCX, 3) }, func() { a.VPslldImm(RAX, RCX, 3) }, func() { a.VPsllqImm(RAX, RCX, 3) },
		func() { a.VPmaddubsw(RAX, RCX, RDX) }, func() { a.Round(RAX, RCX, false, 3) }, func() { a.Ucomis(RAX, RCX, true) },
		func() { a.Cvtsi2f(RAX, RCX, false, true) }, func() { a.Cvttf2si(RAX, RCX, true, false) }, func() { a.MovGprToXmm(RAX, RCX, true) }, func() { a.MovXmmToGpr(RAX, RCX, false) },
		func() { a.FLoadDisp(RAX, RSP, 4, false) }, func() { a.FStoreDisp(RSP, 4, RAX, true) }, func() { a.FLoadIdx(RAX, RCX, RDX, 4, false) }, func() { a.FStoreIdx(RCX, RDX, RAX, 4, true) }, func() { a.SseIdx(0x66, 0x58, RAX, RCX, RDX, 4) },
	}
	for _, f := range emit {
		before := a.Len()
		f()
		if a.Len() <= before {
			t.Fatal("instruction wrapper emitted no bytes")
		}
	}
}

func TestAdditionalSIMDEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want []byte
	}{
		{"addss", func(a *Asm) { a.FAdd(0, 1, false) }, []byte{0xf3, 0x0f, 0x58, 0xc1}},
		{"subsd", func(a *Asm) { a.FSub(0, 1, true) }, []byte{0xf2, 0x0f, 0x5c, 0xc1}},
		{"movss", func(a *Asm) { a.FMov(0, 1, false) }, []byte{0xf3, 0x0f, 0x10, 0xc1}},
		{"vdivsd", func(a *Asm) { a.VFDiv(0, 1, 2, true) }, []byte{0xc4, 0xe1, 0x73, 0x5e, 0xc2}},
		{"vsqrtsd", func(a *Asm) { a.VFSqrt(0, 1, 2, true) }, []byte{0xc4, 0xe1, 0x73, 0x51, 0xc2}},
		{"vcvtdq2ps", func(a *Asm) { a.Vcvtdq2ps(0, 1) }, []byte{0xc4, 0xe1, 0x78, 0x5b, 0xc1}},
		{"vcvtdq2pd", func(a *Asm) { a.Vcvtdq2pd(0, 1) }, []byte{0xc4, 0xe1, 0x7a, 0xe6, 0xc1}},
		{"vcvtps2pd", func(a *Asm) { a.Vcvtps2pd(0, 1) }, []byte{0xc4, 0xe1, 0x78, 0x5a, 0xc1}},
		{"vcvtpd2ps", func(a *Asm) { a.Vcvtpd2ps(0, 1) }, []byte{0xc4, 0xe1, 0x79, 0x5a, 0xc1}},
		{"vshufps", func(a *Asm) { a.VShufps(0, 1, 2, 0x1b) }, []byte{0xc4, 0xe1, 0x70, 0xc6, 0xc2, 0x1b}},
		{"movdqu rip", func(a *Asm) { a.MovdquRipPlaceholder(8) }, []byte{0xf3, 0x44, 0x0f, 0x6f, 0x05, 0, 0, 0, 0}},
		{"movsd rip", func(a *Asm) { a.MovsRipPlaceholder(8, true) }, []byte{0xf2, 0x44, 0x0f, 0x10, 0x05, 0, 0, 0, 0}},
		{"emit bytes", func(a *Asm) { a.EmitBytes([]byte{1, 2, 3}) }, []byte{1, 2, 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			if !bytes.Equal(a.B, tc.want) {
				t.Fatalf("encoding = %x, want %x", a.B, tc.want)
			}
		})
	}
}

func TestIndexAndPlaceholderCompatibilityHelpers(t *testing.T) {
	var a Asm
	a.Pop(R8)
	a.ImulIdx(RAX, RCX, RDX, 4, true)
	a.LeaDispW(R9, RSP, 8, false)
	a.SseRR(0x66, 0x58, R8, R9, false)
	a.LoadIdx(R10, R11, R12, 0, 8, false, true)
	a.LoadIdx(R10, R11, R12, 4, 4, true, true)
	a.LoadIdx(R10, R11, R12, 4, 1, true, false)
	a.LoadIdx(R10, R11, R12, 4, 2, false, false)
	if at := a.JmpPlaceholder(); at != a.Len()-4 {
		t.Fatalf("JmpPlaceholder patch offset = %d, length = %d", at, a.Len())
	}
	before := a.Len()
	a.Align16()
	if a.Len()%16 != 0 || a.Len() < before {
		t.Fatalf("Align16 length = %d", a.Len())
	}
}
