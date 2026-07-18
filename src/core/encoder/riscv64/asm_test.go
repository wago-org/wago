package riscv64

import (
	"math/rand"
	"testing"
)

// Golden words in these tests are cross-checked against Go's riscv64 assembler
// testdata (cmd/asm/internal/asm/testdata/riscv64.s), which in turn tracks the
// unprivileged ISA encodings. Each case emits exactly one 32-bit instruction.

func word(a *Asm) uint32 {
	if len(a.B) != 4 {
		panic("expected exactly one 4-byte instruction")
	}
	return uint32(a.B[0]) | uint32(a.B[1])<<8 | uint32(a.B[2])<<16 | uint32(a.B[3])<<24
}

func TestIntegerEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want uint32
	}{
		{"add x7,x5,x6", func(a *Asm) { a.Add(X7, X5, X6) }, 0x006283b3},
		{"sub x7,x5,x6", func(a *Asm) { a.Sub(X7, X5, X6) }, 0x406283b3},
		{"slt x7,x5,x6", func(a *Asm) { a.Slt(X7, X5, X6) }, 0x0062a3b3},
		{"sltu x7,x5,x6", func(a *Asm) { a.Sltu(X7, X5, X6) }, 0x0062b3b3},
		{"and x7,x5,x6", func(a *Asm) { a.And(X7, X5, X6) }, 0x0062f3b3},
		{"or x7,x5,x6", func(a *Asm) { a.Or(X7, X5, X6) }, 0x0062e3b3},
		{"xor x7,x5,x6", func(a *Asm) { a.Xor(X7, X5, X6) }, 0x0062c3b3},
		{"sll x7,x5,x6", func(a *Asm) { a.Sll(X7, X5, X6) }, 0x006293b3},
		{"srl x7,x5,x6", func(a *Asm) { a.Srl(X7, X5, X6) }, 0x0062d3b3},
		{"sra x7,x5,x6", func(a *Asm) { a.Sra(X7, X5, X6) }, 0x4062d3b3},
		{"addw x7,x6,x5", func(a *Asm) { a.Addw(X7, X6, X5) }, 0x005303bb},
		{"subw x7,x6,x5", func(a *Asm) { a.Subw(X7, X6, X5) }, 0x405303bb},
		{"sllw x7,x6,x5", func(a *Asm) { a.Sllw(X7, X6, X5) }, 0x005313bb},
		{"srlw x7,x6,x5", func(a *Asm) { a.Srlw(X7, X6, X5) }, 0x005353bb},
		{"sraw x7,x6,x5", func(a *Asm) { a.Sraw(X7, X6, X5) }, 0x405353bb},
		{"addi x6,x5,2047", func(a *Asm) { must(t, a.Addi(X6, X5, 2047)) }, 0x7ff28313},
		{"addi x6,x5,-2048", func(a *Asm) { must(t, a.Addi(X6, X5, -2048)) }, 0x80028313},
		{"slti x7,x5,55", func(a *Asm) { must(t, a.Slti(X7, X5, 55)) }, 0x0372a393},
		{"sltiu x7,x5,55", func(a *Asm) { must(t, a.Sltiu(X7, X5, 55)) }, 0x0372b393},
		{"andi x6,x5,1", func(a *Asm) { must(t, a.Andi(X6, X5, 1)) }, 0x0012f313},
		{"ori x6,x5,1", func(a *Asm) { must(t, a.Ori(X6, X5, 1)) }, 0x0012e313},
		{"xori x6,x5,1", func(a *Asm) { must(t, a.Xori(X6, X5, 1)) }, 0x0012c313},
		{"slli x6,x5,1", func(a *Asm) { must(t, a.Slli(X6, X5, 1)) }, 0x00129313},
		{"srli x6,x5,1", func(a *Asm) { must(t, a.Srli(X6, X5, 1)) }, 0x0012d313},
		{"srai x6,x5,1", func(a *Asm) { must(t, a.Srai(X6, X5, 1)) }, 0x4012d313},
		{"addiw x6,x5,1", func(a *Asm) { must(t, a.Addiw(X6, X5, 1)) }, 0x0012831b},
		{"slliw x6,x5,1", func(a *Asm) { must(t, a.Slliw(X6, X5, 1)) }, 0x0012931b},
		{"srliw x6,x5,1", func(a *Asm) { must(t, a.Srliw(X6, X5, 1)) }, 0x0012d31b},
		{"sraiw x6,x5,1", func(a *Asm) { must(t, a.Sraiw(X6, X5, 1)) }, 0x4012d31b},
		{"auipc x10,1", func(a *Asm) { must(t, a.Auipc(X10, 1)) }, 0x00001517},
		{"lui x15,167", func(a *Asm) { must(t, a.Lui(X15, 167)) }, 0x000a77b7},
		{"mul x7,x6,x5", func(a *Asm) { a.Mul(X7, X6, X5) }, 0x025303b3},
		{"mulh x7,x6,x5", func(a *Asm) { a.Mulh(X7, X6, X5) }, 0x025313b3},
		{"mulhu x7,x6,x5", func(a *Asm) { a.Mulhu(X7, X6, X5) }, 0x025333b3},
		{"mulhsu x7,x6,x5", func(a *Asm) { a.Mulhsu(X7, X6, X5) }, 0x025323b3},
		{"div x7,x6,x5", func(a *Asm) { a.Div(X7, X6, X5) }, 0x025343b3},
		{"divu x7,x6,x5", func(a *Asm) { a.Divu(X7, X6, X5) }, 0x025353b3},
		{"rem x7,x6,x5", func(a *Asm) { a.Rem(X7, X6, X5) }, 0x025363b3},
		{"remu x7,x6,x5", func(a *Asm) { a.Remu(X7, X6, X5) }, 0x025373b3},
		{"mulw x7,x6,x5", func(a *Asm) { a.Mulw(X7, X6, X5) }, 0x025303bb},
		{"divw x7,x6,x5", func(a *Asm) { a.Divw(X7, X6, X5) }, 0x025343bb},
		{"divuw x7,x6,x5", func(a *Asm) { a.Divuw(X7, X6, X5) }, 0x025353bb},
		{"remw x7,x6,x5", func(a *Asm) { a.Remw(X7, X6, X5) }, 0x025363bb},
		{"remuw x7,x6,x5", func(a *Asm) { a.Remuw(X7, X6, X5) }, 0x025373bb},
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

func TestLoadStoreEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want uint32
	}{
		{"lb x6,4(x5)", func(a *Asm) { must(t, a.Lb(X6, X5, 4)) }, 0x00428303},
		{"lbu x6,4(x5)", func(a *Asm) { must(t, a.Lbu(X6, X5, 4)) }, 0x0042c303},
		{"lh x6,4(x5)", func(a *Asm) { must(t, a.Lh(X6, X5, 4)) }, 0x00429303},
		{"lhu x6,4(x5)", func(a *Asm) { must(t, a.Lhu(X6, X5, 4)) }, 0x0042d303},
		{"lw x6,4(x5)", func(a *Asm) { must(t, a.Lw(X6, X5, 4)) }, 0x0042a303},
		{"lwu x6,4(x5)", func(a *Asm) { must(t, a.Lwu(X6, X5, 4)) }, 0x0042e303},
		{"ld x6,4(x5)", func(a *Asm) { must(t, a.Ld(X6, X5, 4)) }, 0x0042b303},
		{"sb x5,4(x6)", func(a *Asm) { must(t, a.Sb(X5, X6, 4)) }, 0x00530223},
		{"sh x5,4(x6)", func(a *Asm) { must(t, a.Sh(X5, X6, 4)) }, 0x00531223},
		{"sw x5,4(x6)", func(a *Asm) { must(t, a.Sw(X5, X6, 4)) }, 0x00532223},
		{"sd x5,4(x6)", func(a *Asm) { must(t, a.Sd(X5, X6, 4)) }, 0x00533223},
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

func TestBranchJumpAndSystemEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want uint32
	}{
		{"jalr x6,4(x5)", func(a *Asm) { must(t, a.Jalr(X6, X5, 4)) }, 0x00428367},
		{"ret", func(a *Asm) { a.Ret() }, 0x00008067},
		{"br x5", func(a *Asm) { a.Br(X5) }, 0x00028067},
		{"blr x5", func(a *Asm) { a.Blr(X5) }, 0x000280e7},
		{"fence", func(a *Asm) { a.Fence() }, 0x0ff0000f},
		{"fence.i", func(a *Asm) { a.FenceI() }, 0x0000100f},
		{"ecall", func(a *Asm) { a.Ecall() }, 0x00000073},
		{"ebreak", func(a *Asm) { a.Ebreak() }, 0x00100073},
		{"nop", func(a *Asm) { a.Nop() }, 0x00000013},
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

func TestPatchBranchesAndJumps(t *testing.T) {
	branchWords := []struct {
		cond Cond
		want uint32
	}{
		{CondEQ, 0x00628463},
		{CondNE, 0x00629463},
		{CondLT, 0x0062c463},
		{CondGE, 0x0062d463},
		{CondLTU, 0x0062e463},
		{CondGEU, 0x0062f463},
	}
	for _, tc := range branchWords {
		var a Asm
		at := a.Bcond(X5, X6, tc.cond)
		if !a.PatchBranch13(at, at+8) {
			t.Fatalf("condition %d: patch rejected", tc.cond)
		}
		if got := a.wordAt(at); got != tc.want {
			t.Fatalf("condition %d: got %#08x, want %#08x", tc.cond, got, tc.want)
		}
		if tc.cond.Invert().Invert() != tc.cond {
			t.Fatalf("condition %d does not round-trip through Invert", tc.cond)
		}
	}

	var a Asm
	at := a.Jal(X5)
	if !a.PatchJAL21(at, at+8) || a.wordAt(at) != 0x008002ef {
		t.Fatalf("patched JAL = %#08x", a.wordAt(at))
	}
	if !a.PatchJAL21(at, at-8) || a.wordAt(at) != 0xff9ff2ef {
		t.Fatalf("repatched JAL = %#08x", a.wordAt(at))
	}

	var b Asm
	bat := b.Beq(X5, X6)
	before := b.wordAt(bat)
	for _, target := range []int{bat + 1, bat + 4096, bat - 4098} {
		if b.PatchBranch13(bat, target) {
			t.Fatalf("accepted invalid branch target %d", target)
		}
		if b.wordAt(bat) != before {
			t.Fatal("failed branch patch mutated the instruction")
		}
	}
	var j Asm
	jat := j.Jump()
	jbefore := j.wordAt(jat)
	for _, target := range []int{jat + 1, jat + 1<<20, jat - (1 << 20) - 2} {
		if j.PatchJAL21(jat, target) {
			t.Fatalf("accepted invalid JAL target %d", target)
		}
		if j.wordAt(jat) != jbefore {
			t.Fatal("failed JAL patch mutated the instruction")
		}
	}
}

func TestFarControlTransferPatching(t *testing.T) {
	var jump Asm
	jat := jump.FarCall(T6)
	if len(jump.B) != 8 {
		t.Fatalf("far call length = %d", len(jump.B))
	}
	if !jump.PatchFarJump(jat, 0x123456) {
		t.Fatal("far call patch rejected")
	}
	hi := signExtend(uint64(jump.wordAt(jat)&0xfffff000), 32)
	lo := signExtend(uint64(jump.wordAt(jat+4)>>20), 12)
	if got := int64(jat) + hi + lo; got != 0x123456 {
		t.Fatalf("far call target = %#x", got)
	}
	if jump.wordAt(jat+4)&0xfff != 0x0e7 { // JALR RA,T6,imm
		t.Fatalf("far call linkage word = %#08x", jump.wordAt(jat+4))
	}

	var branch Asm
	bat := branch.FarBcond(X5, X6, CondLT, T6)
	if len(branch.B) != 12 {
		t.Fatalf("far branch length = %d", len(branch.B))
	}
	// Inverse of LT is GE, and the short branch skips all three words.
	if got := branch.wordAt(bat); got != 0x0062d663 {
		t.Fatalf("far branch skip = %#08x, want %#08x", got, uint32(0x0062d663))
	}
	if !branch.PatchFarBranch(bat, -0x123456) {
		t.Fatal("far branch patch rejected")
	}
	pair := bat + 4
	hi = signExtend(uint64(branch.wordAt(pair)&0xfffff000), 32)
	lo = signExtend(uint64(branch.wordAt(pair+4)>>20), 12)
	if got := int64(pair) + hi + lo; got != -0x123456 {
		t.Fatalf("far branch target = %#x", got)
	}
	if branch.wordAt(pair+4)&0xfff != 0x067 { // JALR Zero,T6,imm
		t.Fatalf("far branch jump word = %#08x", branch.wordAt(pair+4))
	}
}

func TestAdrPatch(t *testing.T) {
	for _, target := range []int{0, 4, 0x123456, -0x123456, 0x7ffff000, -0x7ffff000} {
		var a Asm
		at := a.Adr(X9)
		if !a.PatchAdr(at, target) {
			t.Fatalf("target %#x rejected", target)
		}
		hi := signExtend(uint64(a.wordAt(at)&0xfffff000), 32)
		lo := signExtend(uint64(a.wordAt(at+4)>>20), 12)
		got := int64(at) + hi + lo
		if got != int64(target) {
			t.Fatalf("target %#x reconstructed as %#x", target, got)
		}
	}
	var a Asm
	at := a.Adr(X9)
	before0, before1 := a.wordAt(at), a.wordAt(at+4)
	if a.PatchAdr(at, 1<<31) {
		t.Fatal("out-of-range ADR accepted")
	}
	if a.wordAt(at) != before0 || a.wordAt(at+4) != before1 {
		t.Fatal("failed ADR patch mutated the pair")
	}
}

func TestImmediateRangeFailuresDoNotEmit(t *testing.T) {
	checks := []func(*Asm) bool{
		func(a *Asm) bool { return a.Addi(X1, X2, 2048) },
		func(a *Asm) bool { return a.Addi(X1, X2, -2049) },
		func(a *Asm) bool { return a.SubImm64(X1, X2, -2048) },
		func(a *Asm) bool { return a.Lui(X1, 1<<19) },
		func(a *Asm) bool { return a.Lui(X1, -(1<<19)-1) },
		func(a *Asm) bool { return a.Slli(X1, X2, 64) },
		func(a *Asm) bool { return a.Slliw(X1, X2, 32) },
		func(a *Asm) bool { return a.Ld(X1, X2, 2048) },
		func(a *Asm) bool { return a.Sd(X1, X2, -2049) },
		func(a *Asm) bool { return a.Jalr(X1, X2, 2048) },
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

func TestMovImm64RoundTrip(t *testing.T) {
	values := []uint64{
		0, 1, 0x7ff, 0x800, 0xfff, 0x1000,
		0x7fffffff, 0x80000000, 0xffffffff, 0x1_0000_0000,
		0x1234_5678_9abc_def0, 0x8000_0000_0000_0000,
		0xffff_ffff_ffff_f800, 0xffff_ffff_ffff_ffff,
	}
	rng := rand.New(rand.NewSource(1))
	for range 10_000 {
		values = append(values, rng.Uint64())
	}
	for _, want := range values {
		var a Asm
		a.MovImm64(X7, want)
		if len(a.B) == 0 || len(a.B)%4 != 0 {
			t.Fatalf("value %#x emitted %d bytes", want, len(a.B))
		}
		if n := len(a.B) / 4; n > 8 {
			t.Fatalf("value %#x emitted %d instructions, want <= 8", want, n)
		}
		if got := executeImmediateSequence(t, a.B, X7); got != want {
			t.Fatalf("value %#x reconstructed as %#x", want, got)
		}
	}
}

func TestPseudoHelpersAndLayout(t *testing.T) {
	var a Asm
	a.MovReg64(X1, X2)
	a.MovReg32(X3, X4)
	a.Neg64(X5, X6)
	a.Neg32(X7, X8)
	a.Not(X9, X10)
	a.Seqz(X11, X12)
	a.Snez(X13, X14)
	a.Sext8(X15, X16)
	a.Sext16(X17, X18)
	a.Sext32(X19, X20)
	a.Zext32(X21, X22)
	a.MovImm32(X23, -1)
	a.MovSigned32(X24, -1)
	if len(a.B) == 0 || len(a.B)%4 != 0 {
		t.Fatalf("helpers emitted %d bytes", len(a.B))
	}

	var aligned Asm
	aligned.Nop()
	aligned.Align16()
	if len(aligned.B) != 16 {
		t.Fatalf("Align16 length = %d", len(aligned.B))
	}
	var grown Asm
	grown.Grow(128)
	if cap(grown.B) < 128 {
		t.Fatalf("Grow capacity = %d", cap(grown.B))
	}
	grown.Nop()
	grown.Grow(128)
	if cap(grown.B)-len(grown.B) < 128 {
		t.Fatalf("Grow additional capacity = %d", cap(grown.B)-len(grown.B))
	}
	grown.PatchU32(0, 0x12345678)
	if grown.wordAt(0) != 0x12345678 {
		t.Fatalf("PatchU32 = %#08x", grown.wordAt(0))
	}
}

func must(t *testing.T, ok bool) {
	t.Helper()
	if !ok {
		t.Fatal("encoding unexpectedly rejected")
	}
}

func signExtend(v uint64, bits uint) int64 {
	shift := 64 - bits
	return int64(v<<shift) >> shift
}

// executeImmediateSequence interprets the four instructions MovImm64 emits. It
// is intentionally independent of movImmSigned64's chunk-selection algorithm.
func executeImmediateSequence(t *testing.T, code []byte, target Reg) uint64 {
	t.Helper()
	var regs [32]uint64
	for at := 0; at < len(code); at += 4 {
		w := uint32(code[at]) | uint32(code[at+1])<<8 | uint32(code[at+2])<<16 | uint32(code[at+3])<<24
		op := w & 0x7f
		rd := Reg(w >> 7 & 31)
		rs1 := Reg(w >> 15 & 31)
		if rd == Zero {
			continue
		}
		switch op {
		case 0x13:
			funct3 := w >> 12 & 7
			switch funct3 {
			case 0: // ADDI
				imm := signExtend(uint64(w>>20), 12)
				regs[rd] = regs[rs1] + uint64(imm)
			case 1: // SLLI
				regs[rd] = regs[rs1] << ((w >> 20) & 63)
			default:
				t.Fatalf("unexpected OP-IMM funct3 %d in %#08x", funct3, w)
			}
		case 0x1b: // ADDIW
			imm := signExtend(uint64(w>>20), 12)
			v := uint32(regs[rs1] + uint64(imm))
			regs[rd] = uint64(int64(int32(v)))
		case 0x37: // LUI
			v := int64(int32(w & 0xfffff000))
			regs[rd] = uint64(v)
		default:
			t.Fatalf("unexpected immediate materialization opcode %#x in %#08x", op, w)
		}
		regs[Zero] = 0
	}
	return regs[target]
}
