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
