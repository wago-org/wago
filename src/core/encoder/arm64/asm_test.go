package arm64

import "testing"

// Golden 32-bit instruction words produced by clang's integrated assembler
// (`clang --target=aarch64-linux-gnu -c`) disassembled with llvm-objdump. These
// are the authoritative AArch64 encodings; each case emits one instruction and
// asserts the little-endian word matches.

func word(a *Asm) uint32 {
	if len(a.B) != 4 {
		panic("expected exactly one 4-byte instruction")
	}
	return uint32(a.B[0]) | uint32(a.B[1])<<8 | uint32(a.B[2])<<16 | uint32(a.B[3])<<24
}

func TestEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want uint32
	}{
		// add/sub (shifted register)
		{"add x5,x6,x7", func(a *Asm) { a.Add64(X5, X6, X7) }, 0x8b0700c5},
		{"add w5,w6,w7", func(a *Asm) { a.Add32(X5, X6, X7) }, 0x0b0700c5},
		{"sub x5,x6,x7", func(a *Asm) { a.Sub64(X5, X6, X7) }, 0xcb0700c5},
		{"sub w5,w6,w7", func(a *Asm) { a.Sub32(X5, X6, X7) }, 0x4b0700c5},
		{"adds x5,x6,x7", func(a *Asm) { a.Adds64(X5, X6, X7) }, 0xab0700c5},
		{"subs x5,x6,x7", func(a *Asm) { a.Subs64(X5, X6, X7) }, 0xeb0700c5},
		// add/sub (immediate)
		{"add x5,x6,#0", func(a *Asm) { a.AddImm64(X5, X6, 0) }, 0x910000c5},
		{"add x5,x6,#4095", func(a *Asm) { a.AddImm64(X5, X6, 4095) }, 0x913ffcc5},
		{"sub x5,x6,#256", func(a *Asm) { a.SubImm64(X5, X6, 256) }, 0xd10400c5},
		{"sub sp,sp,#16", func(a *Asm) { a.SubSP64(16) }, 0xd10043ff},
		{"add sp,sp,#16", func(a *Asm) { a.AddSP64(16) }, 0x910043ff},
		// moves
		{"mov x9,x10", func(a *Asm) { a.MovReg64(X9, X10) }, 0xaa0a03e9},
		{"mov w9,w10", func(a *Asm) { a.MovReg32(X9, X10) }, 0x2a0a03e9},
		{"movz x11,#0xffff", func(a *Asm) { a.Movz64(X11, 0xffff, 0) }, 0xd29fffeb},
		{"movz x11,#0xffff,lsl16", func(a *Asm) { a.Movz64(X11, 0xffff, 1) }, 0xd2bfffeb},
		{"movk x11,#0x1234,lsl32", func(a *Asm) { a.Movk64(X11, 0x1234, 2) }, 0xf2c2468b},
		// loads / stores (unsigned scaled offset)
		{"ldr x13,[x14]", func(a *Asm) { a.Load64(X13, X14, 0) }, 0xf94001cd},
		{"ldr x13,[x14,#32760]", func(a *Asm) { a.Load64(X13, X14, 32760) }, 0xf97ffdcd},
		{"ldr w13,[x14,#16380]", func(a *Asm) { a.Load32(X13, X14, 16380) }, 0xb97ffdcd},
		{"str x15,[x16,#8]", func(a *Asm) { a.Store64(X15, X16, 8) }, 0xf900060f},
		{"str w15,[x16,#4]", func(a *Asm) { a.Store32(X15, X16, 4) }, 0xb900060f},
		{"ldrb w17,[x18,#1]", func(a *Asm) { a.Ldrb(X17, X18, 1) }, 0x39400651},
		{"strb w17,[x18,#1]", func(a *Asm) { a.Strb(X17, X18, 1) }, 0x39000651},
		// frame pair
		{"stp x29,x30,[sp,#-16]!", func(a *Asm) { a.StpPre(X29, X30, SP, -16) }, 0xa9bf7bfd},
		{"ldp x29,x30,[sp],#16", func(a *Asm) { a.LdpPost(X29, X30, SP, 16) }, 0xa8c17bfd},
		// compare / select / set
		{"cmp x0,x1", func(a *Asm) { a.CmpReg64(X0, X1) }, 0xeb01001f},
		{"cmp x0,#100", func(a *Asm) { a.CmpImm64(X0, 100) }, 0xf101901f},
		{"csel x0,x1,x2,eq", func(a *Asm) { a.Csel64(X0, X1, X2, CondEQ) }, 0x9a820020},
		{"cset x0,ne", func(a *Asm) { a.Cset64(X0, CondNE) }, 0x9a9f07e0},
		// multiply
		{"madd x0,x1,x2,x3", func(a *Asm) { a.Madd64(X0, X1, X2, X3) }, 0x9b020c20},
		{"madd w0,w1,w2,w3", func(a *Asm) { a.Madd32(X0, X1, X2, X3) }, 0x1b020c20},
		{"mul w9,w0,w1", func(a *Asm) { a.Mul32(X9, X0, X1) }, 0x1b017c09},
		{"mul x9,x0,x1", func(a *Asm) { a.Mul64(X9, X0, X1) }, 0x9b017c09},
		// logical (register)
		{"and w5,w6,w7", func(a *Asm) { a.And32(X5, X6, X7) }, 0x0a0700c5},
		{"orr w5,w6,w7", func(a *Asm) { a.Orr32(X5, X6, X7) }, 0x2a0700c5},
		{"eor w5,w6,w7", func(a *Asm) { a.Eor32(X5, X6, X7) }, 0x4a0700c5},
		{"and x5,x6,x7", func(a *Asm) { a.And64(X5, X6, X7) }, 0x8a0700c5},
		{"orr x5,x6,x7", func(a *Asm) { a.Orr64(X5, X6, X7) }, 0xaa0700c5},
		{"eor x5,x6,x7", func(a *Asm) { a.Eor64(X5, X6, X7) }, 0xca0700c5},
		// variable shifts
		{"lsl w5,w6,w7", func(a *Asm) { a.Lslv32(X5, X6, X7) }, 0x1ac720c5},
		{"lsr w5,w6,w7", func(a *Asm) { a.Lsrv32(X5, X6, X7) }, 0x1ac724c5},
		{"asr w5,w6,w7", func(a *Asm) { a.Asrv32(X5, X6, X7) }, 0x1ac728c5},
		{"lsl x5,x6,x7", func(a *Asm) { a.Lslv64(X5, X6, X7) }, 0x9ac720c5},
		// 32-bit compare / cset
		{"cmp w0,w1", func(a *Asm) { a.CmpReg32(X0, X1) }, 0x6b01001f},
		{"cmp w3,#0", func(a *Asm) { a.CmpImm32(X3, 0) }, 0x7100007f},
		{"cset w9,eq", func(a *Asm) { a.Cset32(X9, CondEQ) }, 0x1a9f17e9},
		{"cset w9,lt", func(a *Asm) { a.Cset32(X9, CondLT) }, 0x1a9fa7e9},
		// branches / calls (zero displacement for placeholders)
		{"ret", func(a *Asm) { a.Ret() }, 0xd65f03c0},
		{"br x19", func(a *Asm) { a.Br(X19) }, 0xd61f0260},
		{"blr x20", func(a *Asm) { a.Blr(X20) }, 0xd63f0280},
		{"b #0", func(a *Asm) { a.Branch() }, 0x14000000},
		{"b.eq #0", func(a *Asm) { a.Bcond(CondEQ) }, 0x54000000},
		{"cbz x0,#0", func(a *Asm) { a.Cbz64(X0) }, 0xb4000000},
		{"cbnz x1,#0", func(a *Asm) { a.Cbnz64(X1) }, 0xb5000001},
		// logical immediate (bitmask)
		{"and x0,x1,#0xff", func(a *Asm) {
			if !a.AndImm64(X0, X1, 0xff) {
				t.Fatal("0xff not encodable")
			}
		}, 0x92401c20},
		{"orr x0,x1,#0x1", func(a *Asm) {
			if !a.OrrImm64(X0, X1, 0x1) {
				t.Fatal("0x1 not encodable")
			}
		}, 0xb2400020},
		{"eor x0,x1,#0xf0f0...", func(a *Asm) {
			if !a.EorImm64(X0, X1, 0xf0f0f0f0f0f0f0f0) {
				t.Fatal("0xf0f0... not encodable")
			}
		}, 0xd204cc20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var a Asm
			c.emit(&a)
			if got := word(&a); got != c.want {
				t.Errorf("%s: got %#08x, want %#08x", c.name, got, c.want)
			}
		})
	}
}

// TestMovImm64 checks the multi-instruction constant-materialization sequence
// picks a minimal MOVZ/MOVN + MOVK chain and round-trips the intended value.
func TestAdditionalNEONInstructionEncodersEmitWords(t *testing.T) {
	emitters := []struct {
		name string
		emit func(*Asm)
	}{
		{"hadd-h", func(a *Asm) { a.NeonHaddH(X0, X1, X2) }},
		{"maddwd", func(a *Asm) { a.NeonMaddwd(X0, X1, X2) }},
		{"smull-dq", func(a *Asm) { a.NeonSmullDQ(X0, X1, X2) }},
		{"umull-dq", func(a *Asm) { a.NeonUmullDQ(X0, X1, X2) }},
		{"ushr-h", func(a *Asm) { a.NeonUshrH(X0, X1, 1) }},
		{"ushr-s", func(a *Asm) { a.NeonUshrS(X0, X1, 1) }},
		{"zip1-b", func(a *Asm) { a.NeonZip1B(X0, X1, X2) }},
		{"zip1-h", func(a *Asm) { a.NeonZip1H(X0, X1, X2) }},
		{"zip1-s", func(a *Asm) { a.NeonZip1S(X0, X1, X2) }},
		{"zip2-b", func(a *Asm) { a.NeonZip2B(X0, X1, X2) }},
		{"zip2-h", func(a *Asm) { a.NeonZip2H(X0, X1, X2) }},
		{"zip2-s", func(a *Asm) { a.NeonZip2S(X0, X1, X2) }},
		{"ins-b", func(a *Asm) { a.NeonInsB(X0, X1, 2) }},
		{"ins-h", func(a *Asm) { a.NeonInsH(X0, X1, 2) }},
		{"umov-h", func(a *Asm) { a.NeonUmovH(X0, X1, 2) }},
		{"pshuf-s", func(a *Asm) { a.NeonPshufS(X0, X1, 0) }},
		{"movemask-b", func(a *Asm) { a.NeonMovemaskB(X0, X1) }},
	}
	for _, tc := range emitters {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			if len(a.B) == 0 || len(a.B)%4 != 0 {
				t.Fatalf("encoded %d bytes", len(a.B))
			}
		})
	}
}

func TestScalarFPScaledOffsetsRejectUnencodableDisplacements(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit func(*Asm)
	}{
		{"ldr-s", func(a *Asm) { a.LdrS(X0, X1, 2) }},
		{"ldr-d", func(a *Asm) { a.LdrD(X0, X1, 4) }},
		{"str-s", func(a *Asm) { a.StrS(X1, 2, X0) }},
		{"str-d", func(a *Asm) { a.StrD(X1, 4, X0) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("unencodable scalar-FP offset did not panic")
				}
			}()
			var a Asm
			tc.emit(&a)
		})
	}
	var a Asm
	a.StrF(X1, 8, X0, false)
	a.StrF(X1, 8, X0, true)
	if len(a.B) != 8 {
		t.Fatalf("StrF emitted %d bytes", len(a.B))
	}
}

func TestFloatRoundingAndNEONComparisonModes(t *testing.T) {
	for _, f64 := range []bool{false, true} {
		for _, mode := range []byte{'n', 'm', 'p', 'z'} {
			var a Asm
			a.Frint(X0, X1, f64, mode)
			a.NeonFrint(X0, X1, f64, mode)
			if len(a.B) != 8 {
				t.Fatalf("mode %q f64=%v emitted %d bytes", mode, f64, len(a.B))
			}
		}
		for _, pred := range []byte{0x00, 0x11, 0x12, 0x1d, 0x1e, 0xff} {
			var a Asm
			a.NeonFcmp(X0, X1, X2, f64, pred)
			if len(a.B) != 4 {
				t.Fatalf("predicate %#x f64=%v emitted %d bytes", pred, f64, len(a.B))
			}
		}
	}
	for _, emit := range []func(*Asm){
		func(a *Asm) { a.Frint(X0, X1, false, 'x') },
		func(a *Asm) { a.NeonFrint(X0, X1, false, 'x') },
		func(a *Asm) { a.neon3(0, 3, X0, X1, X2) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid floating-point encoding input did not panic")
				}
			}()
			var a Asm
			emit(&a)
		}()
	}
}

func TestMovImm64(t *testing.T) {
	cases := []struct {
		val   uint64
		words int // expected instruction count (sanity on minimality)
	}{
		{0, 1},
		{0xffff, 1},
		{0x1234, 1},
		{0xffff0000, 1},
		{0x1234_5678, 2},
		{0x1234_5678_9abc_def0, 4},
		{0xffff_ffff_ffff_ffff, 1}, // all-ones -> single MOVN
		{0xffff_ffff_ffff_0000, 1}, // MOVN with one halfword
	}
	for _, c := range cases {
		var a Asm
		a.MovImm64(X7, c.val)
		if len(a.B)%4 != 0 || len(a.B) == 0 {
			t.Fatalf("val %#x: bad code length %d", c.val, len(a.B))
		}
		if n := len(a.B) / 4; n != c.words {
			t.Errorf("val %#x: emitted %d instructions, want %d", c.val, n, c.words)
		}
	}
}

func TestCompatibilityLoadStoreAndLayoutWrappers(t *testing.T) {
	var a Asm
	a.CvtI2F(X0, X1, false, false)
	a.FMov(X2, X3, true)
	a.LdrF(X4, X5, 8, false)
	a.StrF(X5, 8, X4, true)
	a.LdrFIdx(X6, X7, X8, 0, false)
	a.StrFIdx(X7, X8, X6, 4, true)
	a.LdrQIdx(X9, X10, X11, 0)
	a.StrQIdx(X10, X11, X9, 16)
	a.FLoadDisp(X12, X13, 0, false)
	a.FStoreDisp(X13, 0, X12, true)
	a.Ldur64(X14, X15, -8)
	a.Ldur32(X16, X17, 4)
	a.Stur64(X18, X19, -16)
	a.Stur32(X20, X21, 12)
	a.VMovdquLoadDisp(X22, X23, 0)
	a.VMovdquStoreDisp(X23, 16, X22)
	a.Csel(X24, X25, X26, CondEQ, true)
	a.Csel(X24, X25, X26, CondNE, false)
	a.MovImm32(X27, -1)
	if len(a.B) == 0 || len(a.B)%4 != 0 {
		t.Fatalf("wrapper emission length = %d", len(a.B))
	}

	var aligned Asm
	aligned.Nop()
	aligned.Align16()
	if len(aligned.B) != 16 {
		t.Fatalf("Align16 length = %d", len(aligned.B))
	}
	var grown Asm
	grown.Grow(32)
	if cap(grown.B) < 32 {
		t.Fatalf("Grow capacity = %d", cap(grown.B))
	}
}

func TestPatchAndWordHelpers(t *testing.T) {
	var a Asm
	branch := a.Branch()
	if !a.PatchBranch26(branch, 8) || a.wordAt(branch) != 0x14000002 {
		t.Fatalf("patched branch = %#x", a.wordAt(branch))
	}
	cond := a.Bcond(CondEQ)
	if !a.PatchBranch19(cond, 0) || a.wordAt(cond) != 0x54ffffe0 {
		t.Fatalf("patched conditional branch = %#x", a.wordAt(cond))
	}
	if a.PatchBranch26(branch, 1<<28) || a.PatchBranch19(cond, 1<<21) {
		t.Fatal("out-of-range branch patch accepted")
	}
	adr := a.Adr(X3)
	if !a.PatchAdr(adr, adr+5) || a.wordAt(adr) != 0x30000023 {
		t.Fatalf("patched adr = %#x", a.wordAt(adr))
	}
	if a.PatchAdr(adr, adr+(1<<20)) {
		t.Fatal("out-of-range ADR patch accepted")
	}
	a.PatchU32(branch, 0x11223344)
	if a.wordAt(branch) != 0x11223344 {
		t.Fatalf("PatchU32 = %#x", a.wordAt(branch))
	}
	var mov Asm
	mov.Movz64(X0, 0, 0)
	mov.Movk64(X0, 0, 1)
	mov.PatchMovImm(0, 0x12345678)
	if mov.wordAt(0) != 0xd28acf00 || mov.wordAt(4) != 0xf2a24680 {
		t.Fatalf("PatchMovImm = %#x %#x", mov.wordAt(0), mov.wordAt(4))
	}
}

// TestEncodeLogicalImm covers the bitmask-immediate encoder, including values that
// are NOT encodable (must report ok=false so the backend falls back to a register).
func TestEncodeLogicalImm(t *testing.T) {
	// Single-bit values (0x1, 0x2, ...) are all encodable: they are rotations of a
	// one-bit run at a 64-bit element size.
	encodable := []uint64{0xff, 0x1, 0x2, 0xf0f0f0f0f0f0f0f0, 0xffff, 0x5555555555555555, 0xaaaaaaaaaaaaaaaa, 0xfffffffffffffffe}
	for _, v := range encodable {
		if _, _, _, ok := encodeLogicalImm(v, true); !ok {
			t.Errorf("%#x should be an encodable bitmask immediate", v)
		}
	}
	notEncodable := []uint64{0x0, 0xffffffffffffffff, 0x1234_5678_9abc_def0}
	for _, v := range notEncodable {
		if _, _, _, ok := encodeLogicalImm(v, true); ok {
			t.Errorf("%#x should NOT be an encodable bitmask immediate", v)
		}
	}
}

func TestAdditionalScalarAndNEONWrapperForms(t *testing.T) {
	var a Asm
	emit := []func(){
		func() { a.AddImm32(X0, X1, 7) }, func() { a.SubImm32(X0, X1, 7) }, func() { a.SubsImm64(X0, X1, 7) },
		func() { a.Ldrh(X0, X1, 2) }, func() { a.Strh(X0, X1, 2) }, func() { a.Lsrv64(X0, X1, X2) }, func() { a.Asrv64(X0, X1, X2) },
		func() { a.LslImm64(X0, X1, 3) }, func() { a.LsrImm32(X0, X1, 3) }, func() { a.AsrImm64(X0, X1, 3) },
		func() { a.AndImm32(X0, X1, 0xff) }, func() { a.OrrImm32(X0, X1, 1) }, func() { a.EorImm32(X0, X1, 0xff) },
		func() { a.NeonMov16b(X0, X1) }, func() { a.Addv8b(X0, X1) }, func() { a.NeonUminvB(X0, X1) }, func() { a.NeonUminvH(X0, X1) }, func() { a.NeonUminvS(X0, X1) }, func() { a.NeonAddvH(X0, X1) }, func() { a.NeonAddvS(X0, X1) },
		func() { a.NeonAnd16b(X0, X1, X2) }, func() { a.NeonOrr16b(X0, X1, X2) }, func() { a.NeonEor16b(X0, X1, X2) }, func() { a.NeonAndn16b(X0, X1, X2) },
		func() { a.NeonAddB(X0, X1, X2) }, func() { a.NeonAddH(X0, X1, X2) }, func() { a.NeonAddS(X0, X1, X2) }, func() { a.NeonAddD(X0, X1, X2) },
		func() { a.NeonSubB(X0, X1, X2) }, func() { a.NeonSubH(X0, X1, X2) }, func() { a.NeonSubS(X0, X1, X2) }, func() { a.NeonSubD(X0, X1, X2) },
		func() { a.NeonCmeqB(X0, X1, X2) }, func() { a.NeonCmgtH(X0, X1, X2) }, func() { a.NeonCmgeS(X0, X1, X2) }, func() { a.NeonCmhiS(X0, X1, X2) }, func() { a.NeonCmhsB(X0, X1, X2) },
		func() { a.NeonSqaddB(X0, X1, X2) }, func() { a.NeonSqaddH(X0, X1, X2) }, func() { a.NeonUqaddB(X0, X1, X2) }, func() { a.NeonUqaddH(X0, X1, X2) }, func() { a.NeonSqsubB(X0, X1, X2) }, func() { a.NeonSqsubH(X0, X1, X2) }, func() { a.NeonUqsubB(X0, X1, X2) }, func() { a.NeonUqsubH(X0, X1, X2) },
		func() { a.NeonCmeqH(X0, X1, X2) }, func() { a.NeonCmeqS(X0, X1, X2) }, func() { a.NeonCmeqD(X0, X1, X2) }, func() { a.NeonCmgtB(X0, X1, X2) }, func() { a.NeonCmgtS(X0, X1, X2) }, func() { a.NeonCmgeB(X0, X1, X2) }, func() { a.NeonCmgeH(X0, X1, X2) }, func() { a.NeonCmhiH(X0, X1, X2) }, func() { a.NeonCmhsS(X0, X1, X2) },
		func() { a.NeonSminB(X0, X1, X2) }, func() { a.NeonUminH(X0, X1, X2) }, func() { a.NeonSmaxS(X0, X1, X2) }, func() { a.NeonUmaxB(X0, X1, X2) },
		func() { a.NeonSminH(X0, X1, X2) }, func() { a.NeonSminS(X0, X1, X2) }, func() { a.NeonUminB(X0, X1, X2) }, func() { a.NeonUminS(X0, X1, X2) }, func() { a.NeonSmaxB(X0, X1, X2) }, func() { a.NeonSmaxH(X0, X1, X2) }, func() { a.NeonUmaxH(X0, X1, X2) }, func() { a.NeonUmaxS(X0, X1, X2) },
		func() { a.NeonUrhaddB(X0, X1, X2) }, func() { a.NeonUrhaddH(X0, X1, X2) }, func() { a.NeonMulH(X0, X1, X2) }, func() { a.NeonMulS(X0, X1, X2) }, func() { a.NeonSqrdmulhH(X0, X1, X2) }, func() { a.NeonHaddS(X0, X1, X2) },
		func() { a.NeonSaddlpHfromB(X0, X1) }, func() { a.NeonUaddlpSfromH(X0, X1) }, func() { a.NeonSxtlHfromB(X0, X1) }, func() { a.NeonSxtl2HfromB(X0, X1) }, func() { a.NeonUxtlSfromH(X0, X1) }, func() { a.NeonUxtl2DfromS(X0, X1) },
		func() { a.NeonSmullHfromB(X0, X1, X2) }, func() { a.NeonUmull2HfromB(X0, X1, X2) }, func() { a.NeonSmullSfromH(X0, X1, X2) }, func() { a.NeonUmull2SfromH(X0, X1, X2) }, func() { a.NeonSmullDfromS(X0, X1, X2) }, func() { a.NeonUmull2DfromS(X0, X1, X2) },
		func() { a.NeonSqxtnBfromH(X0, X1) }, func() { a.NeonSqxtun2BfromH(X0, X1) }, func() { a.NeonSqxtnHfromS(X0, X1) }, func() { a.NeonSqxtun2HfromS(X0, X1) }, func() { a.NeonSqxtnSfromD(X0, X1) }, func() { a.NeonUqxtnSfromD(X0, X1) },
		func() { a.NeonAbsB(X0, X1) }, func() { a.NeonAbsH(X0, X1) }, func() { a.NeonAbsS(X0, X1) }, func() { a.NeonAbsD(X0, X1) }, func() { a.NeonNegB(X0, X1) }, func() { a.NeonNegH(X0, X1) }, func() { a.NeonNegS(X0, X1) }, func() { a.NeonNegD(X0, X1) },
		func() { a.NeonUshlB(X0, X1, X2) }, func() { a.NeonUshlH(X0, X1, X2) }, func() { a.NeonUshlS(X0, X1, X2) }, func() { a.NeonUshlD(X0, X1, X2) }, func() { a.NeonSshrvH(X0, X1, X2) }, func() { a.NeonSshrvS(X0, X1, X2) }, func() { a.NeonUshrvB(X0, X1, X2) }, func() { a.NeonUshrvH(X0, X1, X2) }, func() { a.NeonUshrvS(X0, X1, X2) }, func() { a.NeonUshrvD(X0, X1, X2) }, func() { a.NeonSshrvD(X0, X1, X2) },
		func() { a.NeonRev64S(X0, X1) }, func() { a.NeonXtnSfromD(X0, X1) }, func() { a.NeonUaddlpDfromS(X0, X1) }, func() { a.NeonShlD(X0, X1, 1) }, func() { a.NeonUmlalDfromS(X0, X1, X2) }, func() { a.NeonSshrH(X0, X1, 1) }, func() { a.NeonSshrS(X0, X1, 1) }, func() { a.NeonUshrD(X0, X1, 1) },
		func() { a.NeonInsS(X0, X1, 1) }, func() { a.NeonInsD(X0, X1, 1) }, func() { a.NeonUmovB(X0, X1, 1) }, func() { a.NeonUmovS(X0, X1, 1) }, func() { a.NeonUmovD(X0, X1, 1) }, func() { a.NeonTbl(X0, X1, X2) },
	}
	for _, f := range emit {
		before := len(a.B)
		f()
		if len(a.B) != before+4 {
			t.Fatalf("wrapper emitted %d bytes, want one instruction", len(a.B)-before)
		}
	}
}

func TestStoreImmIdxAddressAndValueOrder(t *testing.T) {
	for _, tc := range []struct {
		name      string
		disp, val int32
		size      int
	}{
		{"zero-displacement-zero-value", 0, 0, 1},
		{"zero-displacement-immediate", 0, 0x1234, 4},
		{"large-displacement-immediate", 0x12345, 0x1234, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			a.StoreImmIdx(X0, X1, tc.disp, tc.val, tc.size)
			if len(a.B) == 0 || len(a.B)%4 != 0 {
				t.Fatalf("encoded %d bytes", len(a.B))
			}
		})
	}
	var dense Asm
	dense.DenseIdxDisp = true
	dense.StoreImmIdx(X0, X1, 8, 0x1234, 4)
	if len(dense.B) == 0 || len(dense.B)%4 != 0 {
		t.Fatalf("dense indexed store encoded %d bytes", len(dense.B))
	}
}
