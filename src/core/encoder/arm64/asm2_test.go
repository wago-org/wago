package arm64

import "testing"

// Goldens from clang --target=aarch64-linux-gnu + llvm-objdump for the integer
// data-processing port batch (asm2.go).
func TestPortIntEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want uint32
	}{
		{"add x0,x1,x2,lsl#3", func(a *Asm) { a.AddShifted(X0, X1, X2, 3, false) }, 0x8b020c20},
		{"add x0,x1,w2,uxtw", func(a *Asm) { a.AddExtUXTW(X0, X1, X2) }, 0x8b224020},
		{"add x25,x25,w19,uxtw", func(a *Asm) { a.AddExtUXTW(X25, X25, X19) }, 0x8b334339},
		{"adds w0,w1,w2", func(a *Asm) { a.Adds32(X0, X1, X2) }, 0x2b020020},
		{"sxtw x0,w1", func(a *Asm) { a.Sxtw(X0, X1) }, 0x93407c20},
		{"sxtb w0,w1", func(a *Asm) { a.Sxtb(X0, X1, true) }, 0x13001c20},
		{"sxtb x0,w1", func(a *Asm) { a.Sxtb(X0, X1, false) }, 0x93401c20},
		{"sxth w0,w1", func(a *Asm) { a.Sxth(X0, X1, true) }, 0x13003c20},
		{"sxth x0,w1", func(a *Asm) { a.Sxth(X0, X1, false) }, 0x93403c20},
		{"lsl w5,w6,#5", func(a *Asm) { a.LslImm(X5, X6, 5, true) }, 0x531b68c5},
		{"lsl x5,x6,#5", func(a *Asm) { a.LslImm(X5, X6, 5, false) }, 0xd37be8c5},
		{"lsr w5,w6,#5", func(a *Asm) { a.LsrImm(X5, X6, 5, true) }, 0x53057cc5},
		{"lsr x5,x6,#5", func(a *Asm) { a.LsrImm(X5, X6, 5, false) }, 0xd345fcc5},
		{"asr w5,w6,#5", func(a *Asm) { a.AsrImm(X5, X6, 5, true) }, 0x13057cc5},
		{"asr x5,x6,#5", func(a *Asm) { a.AsrImm(X5, X6, 5, false) }, 0x9345fcc5},
		{"ror w5,w6,#5", func(a *Asm) { a.RorImm(X5, X6, 5, true) }, 0x138614c5},
		{"ror x5,x6,#5", func(a *Asm) { a.RorImm(X5, X6, 5, false) }, 0x93c614c5},
		{"ror w0,w1,w2", func(a *Asm) { a.Rorv32(X0, X1, X2) }, 0x1ac22c20},
		{"ror x0,x1,x2", func(a *Asm) { a.Rorv64(X0, X1, X2) }, 0x9ac22c20},
		{"clz w0,w1", func(a *Asm) { a.Clz(X0, X1, true) }, 0x5ac01020},
		{"clz x0,x1", func(a *Asm) { a.Clz(X0, X1, false) }, 0xdac01020},
		{"rbit w0,w1", func(a *Asm) { a.Rbit(X0, X1, true) }, 0x5ac00020},
		{"rbit x0,x1", func(a *Asm) { a.Rbit(X0, X1, false) }, 0xdac00020},
		{"sdiv w0,w1,w2", func(a *Asm) { a.Sdiv32(X0, X1, X2) }, 0x1ac20c20},
		{"sdiv x0,x1,x2", func(a *Asm) { a.Sdiv64(X0, X1, X2) }, 0x9ac20c20},
		{"udiv w0,w1,w2", func(a *Asm) { a.Udiv32(X0, X1, X2) }, 0x1ac20820},
		{"udiv x0,x1,x2", func(a *Asm) { a.Udiv64(X0, X1, X2) }, 0x9ac20820},
		{"msub w0,w1,w2,w3", func(a *Asm) { a.Msub32(X0, X1, X2, X3) }, 0x1b028c20},
		{"msub x0,x1,x2,x3", func(a *Asm) { a.Msub64(X0, X1, X2, X3) }, 0x9b028c20},
		{"cmn w1,#5", func(a *Asm) { a.CmnImm32(X1, 5) }, 0x3100143f},
		{"cmn x1,#5", func(a *Asm) { a.CmnImm64(X1, 5) }, 0xb100143f},
		{"smulh x0,x1,x2", func(a *Asm) { a.Smulh(X0, X1, X2) }, 0x9b427c20},
		{"umulh x0,x1,x2", func(a *Asm) { a.Umulh(X0, X1, X2) }, 0x9bc27c20},
		{"smull x0,w1,w2", func(a *Asm) { a.Smull(X0, X1, X2) }, 0x9b227c20},
		{"umull x0,w1,w2", func(a *Asm) { a.Umull(X0, X1, X2) }, 0x9ba27c20},
		{"csel w0,w1,w2,eq", func(a *Asm) { a.Csel32(X0, X1, X2, CondEQ) }, 0x1a820020},
		{"tst x1,x2", func(a *Asm) { a.TstReg(X1, X2, false) }, 0xea02003f},
		{"tst w1,w2", func(a *Asm) { a.TstReg(X1, X2, true) }, 0x6a02003f},
		{"tst x1,#0x8080808080808080", func(a *Asm) {
			if !a.TstImm64(X1, 0x8080808080808080) {
				t.Fatal("packed high-bit mask not encodable")
			}
		}, 0xf201c03f},
		// 32-bit logical immediate (and w0,w1,#0xff)
		{"and w0,w1,#0xff", func(a *Asm) {
			if !a.AndImm32(X0, X1, 0xff) {
				t.Fatal("0xff not encodable (32-bit)")
			}
		}, 0x12001c20},
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

// Goldens for the scalar-FP + SP + branch batch.
func TestPortFPEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want uint32
	}{
		{"fadd s0,s1,s2", func(a *Asm) { a.Fadd(X0, X1, X2, false) }, 0x1e222820},
		{"fadd d0,d1,d2", func(a *Asm) { a.Fadd(X0, X1, X2, true) }, 0x1e622820},
		{"fsub s0,s1,s2", func(a *Asm) { a.Fsub(X0, X1, X2, false) }, 0x1e223820},
		{"fmul s0,s1,s2", func(a *Asm) { a.Fmul(X0, X1, X2, false) }, 0x1e220820},
		{"fdiv s0,s1,s2", func(a *Asm) { a.Fdiv(X0, X1, X2, false) }, 0x1e221820},
		{"fsqrt s0,s1", func(a *Asm) { a.Fsqrt(X0, X1, false) }, 0x1e21c020},
		{"fmin s0,s1,s2", func(a *Asm) { a.Fmin(X0, X1, X2, false) }, 0x1e225820},
		{"fmax s0,s1,s2", func(a *Asm) { a.Fmax(X0, X1, X2, false) }, 0x1e224820},
		{"fmov s0,s1", func(a *Asm) { a.FmovReg(X0, X1, false) }, 0x1e204020},
		{"fmov d0,d1", func(a *Asm) { a.FmovReg(X0, X1, true) }, 0x1e604020},
		{"fmov s0,w1", func(a *Asm) { a.FmovFromGpr(X0, X1, false) }, 0x1e270020},
		{"fmov d0,x1", func(a *Asm) { a.FmovFromGpr(X0, X1, true) }, 0x9e670020},
		{"fmov w0,s1", func(a *Asm) { a.FmovToGpr(X0, X1, false) }, 0x1e260020},
		{"fmov x0,d1", func(a *Asm) { a.FmovToGpr(X0, X1, true) }, 0x9e660020},
		{"fcmp s0,s1", func(a *Asm) { a.Fcmp(X0, X1, false) }, 0x1e212000},
		{"fcmp d0,d1", func(a *Asm) { a.Fcmp(X0, X1, true) }, 0x1e612000},
		{"frintn s0,s1", func(a *Asm) { a.Frint(X0, X1, false, 'n') }, 0x1e244020},
		{"frintm s0,s1", func(a *Asm) { a.Frint(X0, X1, false, 'm') }, 0x1e254020},
		{"frintp s0,s1", func(a *Asm) { a.Frint(X0, X1, false, 'p') }, 0x1e24c020},
		{"frintz s0,s1", func(a *Asm) { a.Frint(X0, X1, false, 'z') }, 0x1e25c020},
		{"fcvtzs w0,s1", func(a *Asm) { a.Fcvtzs(X0, X1, false, false) }, 0x1e380020},
		{"fcvtzs x0,s1", func(a *Asm) { a.Fcvtzs(X0, X1, false, true) }, 0x9e380020},
		{"fcvtzs w0,d1", func(a *Asm) { a.Fcvtzs(X0, X1, true, false) }, 0x1e780020},
		{"fcvtzs x0,d1", func(a *Asm) { a.Fcvtzs(X0, X1, true, true) }, 0x9e780020},
		{"scvtf s0,w1", func(a *Asm) { a.Scvtf(X0, X1, false, false) }, 0x1e220020},
		{"scvtf d0,w1", func(a *Asm) { a.Scvtf(X0, X1, true, false) }, 0x1e620020},
		{"scvtf s0,x1", func(a *Asm) { a.Scvtf(X0, X1, false, true) }, 0x9e220020},
		{"scvtf d0,x1", func(a *Asm) { a.Scvtf(X0, X1, true, true) }, 0x9e620020},
		{"ucvtf s0,w1", func(a *Asm) { a.Ucvtf(X0, X1, false, false) }, 0x1e230020},
		{"ucvtf d0,w1", func(a *Asm) { a.Ucvtf(X0, X1, true, false) }, 0x1e630020},
		{"ucvtf s0,x1", func(a *Asm) { a.Ucvtf(X0, X1, false, true) }, 0x9e230020},
		{"ucvtf d0,x1", func(a *Asm) { a.Ucvtf(X0, X1, true, true) }, 0x9e630020},
		{"fcvt d0,s1", func(a *Asm) { a.FcvtS2D(X0, X1) }, 0x1e22c020},
		{"fcvt s0,d1", func(a *Asm) { a.FcvtD2S(X0, X1) }, 0x1e624020},
		{"sub sp,sp,x0", func(a *Asm) { a.SubSPReg(X0) }, 0xcb2063ff},
		{"add sp,sp,x0", func(a *Asm) { a.AddSPReg(X0) }, 0x8b2063ff},
		{"cmp sp,x0", func(a *Asm) { a.CmpSP64(X0) }, 0xeb2063ff},
		{"bl 0", func(a *Asm) { a.Bl() }, 0x94000000},
		{"adr x0,0", func(a *Asm) { a.Adr(X0) }, 0x10000000},
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

// Goldens for the load/store addressing-mode batch.
func TestPortLoadStoreEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want uint32
	}{
		{"ldrb w0,[x1,x2]", func(a *Asm) { a.LdrIdx(X0, X1, X2, 1, false, false) }, 0x38626820},
		{"ldrsb x0,[x1,x2]", func(a *Asm) { a.LdrIdx(X0, X1, X2, 1, true, true) }, 0x38a26820},
		{"ldrsh x0,[x1,x2]", func(a *Asm) { a.LdrIdx(X0, X1, X2, 2, true, true) }, 0x78a26820},
		{"ldrsw x0,[x1,x2]", func(a *Asm) { a.LdrIdx(X0, X1, X2, 4, true, true) }, 0xb8a26820},
		{"strb w0,[x1,x2]", func(a *Asm) { a.StrIdx(X0, X1, X2, 1) }, 0x38226820},
		{"ldr s0,[x1,#4]", func(a *Asm) { a.LdrS(X0, X1, 4) }, 0xbd400420},
		{"ldr d0,[x1,#8]", func(a *Asm) { a.LdrD(X0, X1, 8) }, 0xfd400420},
		{"str s0,[x1,#4]", func(a *Asm) { a.StrS(X1, 4, X0) }, 0xbd000420},
		{"str d0,[x1,#8]", func(a *Asm) { a.StrD(X1, 8, X0) }, 0xfd000420},
		{"ldr q0,[x1,#16]", func(a *Asm) { a.LdrQ(X0, X1, 16) }, 0x3dc00420},
		{"str q0,[x1,#16]", func(a *Asm) { a.StrQ(X1, 16, X0) }, 0x3d800420},
		{"add x0,sp,#16", func(a *Asm) { a.LeaSP(X0, 16) }, 0x910043e0},
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

// TestPortDispAddressing verifies LoadIdx/StoreIdx fold an encodable nonzero
// displacement into the load/store after computing base+index (goldens from clang).
func TestPortDispAddressing(t *testing.T) {
	words := func(a *Asm) []uint32 {
		out := make([]uint32, 0, len(a.B)/4)
		for i := 0; i+4 <= len(a.B); i += 4 {
			out = append(out, uint32(a.B[i])|uint32(a.B[i+1])<<8|uint32(a.B[i+2])<<16|uint32(a.B[i+3])<<24)
		}
		return out
	}
	eq := func(t *testing.T, got, want []uint32) {
		if len(got) != len(want) {
			t.Fatalf("len %d != %d (%#x)", len(got), len(want), got)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("[%d] %#08x != %#08x", i, got[i], want[i])
			}
		}
	}
	// LoadIdx w0 = [x28 + x9 + 8]  →  add x16,x28,x9 ; ldr w0,[x16,#8]
	var la Asm
	la.DenseIdxDisp = true
	la.LoadIdx(X0, X28, X9, 8, 4, false, false)
	eq(t, words(&la), []uint32{0x8b090390, 0xb9400a00})
	// StoreIdx [x28 + x9 + 8] = w2  →  add ; str w2,[x16,#8]
	var sa Asm
	sa.DenseIdxDisp = true
	sa.StoreIdx(X28, X9, X2, 8, 4)
	eq(t, words(&sa), []uint32{0x8b090390, 0xb9000a02})
	// disp 0 stays a single reg-offset store (no address fold)
	var s0 Asm
	s0.StoreIdx(X28, X9, X2, 0, 4)
	if len(s0.B) != 4 {
		t.Errorf("disp-0 store should be 1 instruction, got %d bytes", len(s0.B))
	}
}

func TestIndexedMemoryAddressingFallbacks(t *testing.T) {
	for _, dense := range []bool{false, true} {
		for _, tc := range []struct {
			name string
			emit func(*Asm)
		}{
			{"load-plain", func(a *Asm) { a.LoadIdx(X0, X1, X2, 0, 1, false, false) }},
			{"load-fold-or-small", func(a *Asm) { a.LoadIdx(X0, X1, X2, 8, 2, true, true) }},
			{"load-large-negative", func(a *Asm) { a.LoadIdx(X0, X1, X2, -0x12345, 4, true, true) }},
			{"load-large-positive", func(a *Asm) { a.LoadIdx(X0, X1, X2, 0x12345, 8, false, true) }},
			{"store-plain", func(a *Asm) { a.StoreIdx(X1, X2, X0, 0, 1) }},
			{"store-fold-or-small", func(a *Asm) { a.StoreIdx(X1, X2, X0, 8, 2) }},
			{"store-large-negative", func(a *Asm) { a.StoreIdx(X1, X2, X0, -0x12345, 4) }},
			{"store-large-positive", func(a *Asm) { a.StoreIdx(X1, X2, X0, 0x12345, 8) }},
			{"load-f32-index", func(a *Asm) { a.LdrFIdx(X0, X1, X2, 0, false) }},
			{"load-f64-fold", func(a *Asm) { a.LdrFIdx(X0, X1, X2, 8, true) }},
			{"store-f32-index", func(a *Asm) { a.StrFIdx(X1, X2, X0, 0, false) }},
			{"store-f64-large", func(a *Asm) { a.StrFIdx(X1, X2, X0, -0x12345, true) }},
			{"load-q-index", func(a *Asm) { a.LdrQIdx(X0, X1, X2, 0) }},
			{"load-q-fold", func(a *Asm) { a.LdrQIdx(X0, X1, X2, 16) }},
			{"store-q-index", func(a *Asm) { a.StrQIdx(X1, X2, X0, 0) }},
			{"store-q-large", func(a *Asm) { a.StrQIdx(X1, X2, X0, -0x12345) }},
			{"q-spill-large", func(a *Asm) { a.LdrQ(X0, X1, 0x12345); a.StrQ(X1, 0x12345, X0) }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var a Asm
				a.DenseIdxDisp = dense
				tc.emit(&a)
				if len(a.B) == 0 || len(a.B)%4 != 0 {
					t.Fatalf("dense=%v emitted %d bytes", dense, len(a.B))
				}
			})
		}
	}
}

func TestPortNeon16bLogical(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want uint32
	}{
		{"and v0.16b,v1.16b,v2.16b", func(a *Asm) { a.And16b(X0, X1, X2) }, 0x4e221c20},
		{"orr v0.16b,v1.16b,v2.16b", func(a *Asm) { a.Orr16b(X0, X1, X2) }, 0x4ea21c20},
		{"eor v0.16b,v1.16b,v2.16b", func(a *Asm) { a.Eor16b(X0, X1, X2) }, 0x6e221c20},
		{"mvn v0.16b,v1.16b", func(a *Asm) { a.NeonNot16b(X0, X1) }, 0x6e205820},
		{"bsl v0.16b,v1.16b,v2.16b", func(a *Asm) { a.NeonBsl16b(X0, X1, X2) }, 0x6e621c20},
		{"cnt v0.16b,v1.16b", func(a *Asm) { a.NeonCntB(X0, X1) }, 0x4e205820},
		{"umaxv b0,v1.16b", func(a *Asm) { a.NeonUmaxvB(X0, X1) }, 0x6e30a820},
		{"abs v0.2d,v1.2d", func(a *Asm) { a.NeonAbsD(X0, X1) }, 0x4ee0b820},
		{"neg v0.2d,v1.2d", func(a *Asm) { a.NeonNegD(X0, X1) }, 0x6ee0b820},
		{"cmhi v0.16b,v1.16b,v2.16b", func(a *Asm) { a.NeonCmhiB(X0, X1, X2) }, 0x6e223420},
		{"cmhs v0.8h,v1.8h,v2.8h", func(a *Asm) { a.NeonCmhsH(X0, X1, X2) }, 0x6e623c20},
		{"cmge v0.4s,v1.4s,v2.4s", func(a *Asm) { a.NeonCmgeS(X0, X1, X2) }, 0x4ea23c20},
		{"cmgt v0.2d,v1.2d,v2.2d", func(a *Asm) { a.NeonCmgtD(X0, X1, X2) }, 0x4ee23420},
		{"cmge v0.2d,v1.2d,v2.2d", func(a *Asm) { a.NeonCmgeD(X0, X1, X2) }, 0x4ee23c20},
		{"fadd v0.4s,v1.4s,v2.4s", func(a *Asm) { a.NeonFadd(X0, X1, X2, false) }, 0x4e22d420},
		{"fadd v0.2d,v1.2d,v2.2d", func(a *Asm) { a.NeonFadd(X0, X1, X2, true) }, 0x4e62d420},
		{"fsub v0.4s,v1.4s,v2.4s", func(a *Asm) { a.NeonFsub(X0, X1, X2, false) }, 0x4ea2d420},
		{"fsub v0.2d,v1.2d,v2.2d", func(a *Asm) { a.NeonFsub(X0, X1, X2, true) }, 0x4ee2d420},
		{"fmul v0.4s,v1.4s,v2.4s", func(a *Asm) { a.NeonFmul(X0, X1, X2, false) }, 0x6e22dc20},
		{"fmul v0.2d,v1.2d,v2.2d", func(a *Asm) { a.NeonFmul(X0, X1, X2, true) }, 0x6e62dc20},
		{"fdiv v0.4s,v1.4s,v2.4s", func(a *Asm) { a.NeonFdiv(X0, X1, X2, false) }, 0x6e22fc20},
		{"fdiv v0.2d,v1.2d,v2.2d", func(a *Asm) { a.NeonFdiv(X0, X1, X2, true) }, 0x6e62fc20},
		{"fmax v0.4s,v1.4s,v2.4s", func(a *Asm) { a.NeonFmax(X0, X1, X2, false) }, 0x4e22f420},
		{"fmax v0.2d,v1.2d,v2.2d", func(a *Asm) { a.NeonFmax(X0, X1, X2, true) }, 0x4e62f420},
		{"fmin v0.4s,v1.4s,v2.4s", func(a *Asm) { a.NeonFmin(X0, X1, X2, false) }, 0x4ea2f420},
		{"fmin v0.2d,v1.2d,v2.2d", func(a *Asm) { a.NeonFmin(X0, X1, X2, true) }, 0x4ee2f420},
		{"fabs v0.4s,v1.4s", func(a *Asm) { a.NeonFabs(X0, X1, false) }, 0x4ea0f820},
		{"fneg v0.2d,v1.2d", func(a *Asm) { a.NeonFneg(X0, X1, true) }, 0x6ee0f820},
		{"fsqrt v0.4s,v1.4s", func(a *Asm) { a.NeonFsqrt(X0, X1, false) }, 0x6ea1f820},
		{"fsqrt v0.2d,v1.2d", func(a *Asm) { a.NeonFsqrt(X0, X1, true) }, 0x6ee1f820},
		{"fcvtn v0.2s,v1.2d", func(a *Asm) { a.NeonFcvtnSfromD(X0, X1) }, 0x0e616820},
		{"fcvtl v0.2d,v1.2s", func(a *Asm) { a.NeonFcvtlDfromS(X0, X1) }, 0x0e617820},
		{"scvtf v0.4s,v1.4s", func(a *Asm) { a.NeonScvtfSfromS(X0, X1) }, 0x4e21d820},
		{"ucvtf v0.4s,v1.4s", func(a *Asm) { a.NeonUcvtfSfromS(X0, X1) }, 0x6e21d820},
		{"scvtf v0.2d,v1.2d", func(a *Asm) { a.NeonScvtfDfromD(X0, X1) }, 0x4e61d820},
		{"ucvtf v0.2d,v1.2d", func(a *Asm) { a.NeonUcvtfDfromD(X0, X1) }, 0x6e61d820},
		{"fcvtzs v0.4s,v1.4s", func(a *Asm) { a.NeonFcvtzsSfromS(X0, X1) }, 0x4ea1b820},
		{"fcvtzu v2.4s,v3.4s", func(a *Asm) { a.NeonFcvtzuSfromS(X2, X3) }, 0x6ea1b862},
		{"fcvtzs v0.2d,v1.2d", func(a *Asm) { a.NeonFcvtzsDfromD(X0, X1) }, 0x4ee1b820},
		{"fcvtzu v2.2d,v3.2d", func(a *Asm) { a.NeonFcvtzuDfromD(X2, X3) }, 0x6ee1b862},
		{"frintp v0.4s,v1.4s", func(a *Asm) { a.NeonFrint(X0, X1, false, 'p') }, 0x4ea18820},
		{"frintp v0.2d,v1.2d", func(a *Asm) { a.NeonFrint(X0, X1, true, 'p') }, 0x4ee18820},
		{"frintm v0.4s,v1.4s", func(a *Asm) { a.NeonFrint(X0, X1, false, 'm') }, 0x4e219820},
		{"frintm v0.2d,v1.2d", func(a *Asm) { a.NeonFrint(X0, X1, true, 'm') }, 0x4e619820},
		{"frintz v0.4s,v1.4s", func(a *Asm) { a.NeonFrint(X0, X1, false, 'z') }, 0x4ea19820},
		{"frintz v0.2d,v1.2d", func(a *Asm) { a.NeonFrint(X0, X1, true, 'z') }, 0x4ee19820},
		{"frintn v0.4s,v1.4s", func(a *Asm) { a.NeonFrint(X0, X1, false, 'n') }, 0x4e218820},
		{"frintn v0.2d,v1.2d", func(a *Asm) { a.NeonFrint(X0, X1, true, 'n') }, 0x4e618820},
		{"dup v0.16b,v1.b[0]", func(a *Asm) { a.NeonDupB(X0, X1) }, 0x4e010420},
		{"dup v0.8h,v1.h[0]", func(a *Asm) { a.NeonDupH(X0, X1) }, 0x4e020420},
		{"dup v0.4s,v1.s[0]", func(a *Asm) { a.NeonDupS(X0, X1) }, 0x4e040420},
		{"dup v0.2d,v1.d[0]", func(a *Asm) { a.NeonDupD(X0, X1) }, 0x4e080420},
		{"dup v0.16b,w1", func(a *Asm) { a.NeonDupGprB(X0, X1) }, 0x4e010c20},
		{"dup v2.8h,w3", func(a *Asm) { a.NeonDupGprH(X2, X3) }, 0x4e020c62},
		{"dup v4.4s,w5", func(a *Asm) { a.NeonDupGprS(X4, X5) }, 0x4e040ca4},
		{"dup v6.2d,x7", func(a *Asm) { a.NeonDupGprD(X6, X7) }, 0x4e080ce6},
		{"dup v0.4s,v1.s[3]", func(a *Asm) { a.NeonDupLaneS(X0, X1, 3) }, 0x4e1c0420},
		{"dup v2.2d,v3.d[1]", func(a *Asm) { a.NeonDupLaneD(X2, X3, 1) }, 0x4e180462},
		{"ins v4.s[2],v5.s[0]", func(a *Asm) { a.NeonInsLaneS(X4, 2, X5) }, 0x6e1404a4},
		{"ins v6.d[1],v7.d[0]", func(a *Asm) { a.NeonInsLaneD(X6, 1, X7) }, 0x6e1804e6},
		{"saddlp v0.8h,v1.16b", func(a *Asm) { a.NeonSaddlpHfromB(X0, X1) }, 0x4e202820},
		{"uaddlp v0.8h,v1.16b", func(a *Asm) { a.NeonUaddlpHfromB(X0, X1) }, 0x6e202820},
		{"saddlp v0.4s,v1.8h", func(a *Asm) { a.NeonSaddlpSfromH(X0, X1) }, 0x4e602820},
		{"uaddlp v0.4s,v1.8h", func(a *Asm) { a.NeonUaddlpSfromH(X0, X1) }, 0x6e602820},
		{"sxtl v0.8h,v1.8b", func(a *Asm) { a.NeonSxtlHfromB(X0, X1) }, 0x0f08a420},
		{"sxtl2 v0.8h,v1.16b", func(a *Asm) { a.NeonSxtl2HfromB(X0, X1) }, 0x4f08a420},
		{"uxtl v0.8h,v1.8b", func(a *Asm) { a.NeonUxtlHfromB(X0, X1) }, 0x2f08a420},
		{"uxtl2 v0.8h,v1.16b", func(a *Asm) { a.NeonUxtl2HfromB(X0, X1) }, 0x6f08a420},
		{"sxtl v0.4s,v1.4h", func(a *Asm) { a.NeonSxtlSfromH(X0, X1) }, 0x0f10a420},
		{"sxtl2 v0.4s,v1.8h", func(a *Asm) { a.NeonSxtl2SfromH(X0, X1) }, 0x4f10a420},
		{"uxtl v0.4s,v1.4h", func(a *Asm) { a.NeonUxtlSfromH(X0, X1) }, 0x2f10a420},
		{"uxtl2 v0.4s,v1.8h", func(a *Asm) { a.NeonUxtl2SfromH(X0, X1) }, 0x6f10a420},
		{"sxtl v0.2d,v1.2s", func(a *Asm) { a.NeonSxtlDfromS(X0, X1) }, 0x0f20a420},
		{"sxtl2 v0.2d,v1.4s", func(a *Asm) { a.NeonSxtl2DfromS(X0, X1) }, 0x4f20a420},
		{"uxtl v0.2d,v1.2s", func(a *Asm) { a.NeonUxtlDfromS(X0, X1) }, 0x2f20a420},
		{"uxtl2 v0.2d,v1.4s", func(a *Asm) { a.NeonUxtl2DfromS(X0, X1) }, 0x6f20a420},
		{"sqxtn v0.8b,v1.8h", func(a *Asm) { a.NeonSqxtnBfromH(X0, X1) }, 0x0e214820},
		{"sqxtn2 v0.16b,v2.8h", func(a *Asm) { a.NeonSqxtn2BfromH(X0, X2) }, 0x4e214840},
		{"sqxtun v3.8b,v4.8h", func(a *Asm) { a.NeonSqxtunBfromH(X3, X4) }, 0x2e212883},
		{"sqxtun2 v3.16b,v5.8h", func(a *Asm) { a.NeonSqxtun2BfromH(X3, X5) }, 0x6e2128a3},
		{"sqxtn v9.4h,v10.4s", func(a *Asm) { a.NeonSqxtnHfromS(X9, X10) }, 0x0e614949},
		{"sqxtn2 v9.8h,v11.4s", func(a *Asm) { a.NeonSqxtn2HfromS(X9, X11) }, 0x4e614969},
		{"sqxtun v12.4h,v13.4s", func(a *Asm) { a.NeonSqxtunHfromS(X12, X13) }, 0x2e6129ac},
		{"sqxtun2 v12.8h,v14.4s", func(a *Asm) { a.NeonSqxtun2HfromS(X12, X14) }, 0x6e6129cc},
		{"sqxtn v4.2s,v5.2d", func(a *Asm) { a.NeonSqxtnSfromD(X4, X5) }, 0x0ea148a4},
		{"uqxtn v6.2s,v7.2d", func(a *Asm) { a.NeonUqxtnSfromD(X6, X7) }, 0x2ea148e6},
		{"ushr v0.16b,v1.16b,#7", func(a *Asm) { a.NeonUshrB(X0, X1, 7) }, 0x6f090420},
		{"addp v0.4s,v1.4s,v2.4s", func(a *Asm) { a.NeonAddpS(X0, X1, X2) }, 0x4ea2bc20},
		{"smull v0.8h,v1.8b,v2.8b", func(a *Asm) { a.NeonSmullHfromB(X0, X1, X2) }, 0x0e22c020},
		{"smull2 v0.8h,v1.16b,v2.16b", func(a *Asm) { a.NeonSmull2HfromB(X0, X1, X2) }, 0x4e22c020},
		{"umull v0.8h,v1.8b,v2.8b", func(a *Asm) { a.NeonUmullHfromB(X0, X1, X2) }, 0x2e22c020},
		{"umull2 v0.8h,v1.16b,v2.16b", func(a *Asm) { a.NeonUmull2HfromB(X0, X1, X2) }, 0x6e22c020},
		{"smull v0.4s,v1.4h,v2.4h", func(a *Asm) { a.NeonSmullSfromH(X0, X1, X2) }, 0x0e62c020},
		{"smull2 v0.4s,v1.8h,v2.8h", func(a *Asm) { a.NeonSmull2SfromH(X0, X1, X2) }, 0x4e62c020},
		{"umull v0.4s,v1.4h,v2.4h", func(a *Asm) { a.NeonUmullSfromH(X0, X1, X2) }, 0x2e62c020},
		{"umull2 v0.4s,v1.8h,v2.8h", func(a *Asm) { a.NeonUmull2SfromH(X0, X1, X2) }, 0x6e62c020},
		{"smull v0.2d,v1.2s,v2.2s", func(a *Asm) { a.NeonSmullDfromS(X0, X1, X2) }, 0x0ea2c020},
		{"smull2 v0.2d,v1.4s,v2.4s", func(a *Asm) { a.NeonSmull2DfromS(X0, X1, X2) }, 0x4ea2c020},
		{"umull v0.2d,v1.2s,v2.2s", func(a *Asm) { a.NeonUmullDfromS(X0, X1, X2) }, 0x2ea2c020},
		{"umull2 v0.2d,v1.4s,v2.4s", func(a *Asm) { a.NeonUmull2DfromS(X0, X1, X2) }, 0x6ea2c020},
		{"ushl v0.16b,v1.16b,v2.16b", func(a *Asm) { a.NeonUshlB(X0, X1, X2) }, 0x6e224420},
		{"sshl v3.16b,v4.16b,v5.16b", func(a *Asm) { a.NeonSshrvB(X3, X4, X5) }, 0x4e254483},
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
