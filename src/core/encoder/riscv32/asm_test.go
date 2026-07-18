package riscv32

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func word(a *Asm) uint32 {
	if len(a.B) != 4 {
		panic("expected one instruction")
	}
	return uint32(a.B[0]) | uint32(a.B[1])<<8 | uint32(a.B[2])<<16 | uint32(a.B[3])<<24
}
func must(t *testing.T, ok bool) {
	t.Helper()
	if !ok {
		t.Fatal("encoding rejected")
	}
}

func TestRV32IntegerEncodings(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want uint32
	}{
		{"add", func(a *Asm) { a.Add(X7, X5, X6) }, 0x006283b3},
		{"sub", func(a *Asm) { a.Sub(X7, X5, X6) }, 0x406283b3},
		{"slt", func(a *Asm) { a.Slt(X7, X5, X6) }, 0x0062a3b3},
		{"sltu", func(a *Asm) { a.Sltu(X7, X5, X6) }, 0x0062b3b3},
		{"and", func(a *Asm) { a.And(X7, X5, X6) }, 0x0062f3b3},
		{"or", func(a *Asm) { a.Or(X7, X5, X6) }, 0x0062e3b3},
		{"xor", func(a *Asm) { a.Xor(X7, X5, X6) }, 0x0062c3b3},
		{"sll", func(a *Asm) { a.Sll(X7, X5, X6) }, 0x006293b3},
		{"srl", func(a *Asm) { a.Srl(X7, X5, X6) }, 0x0062d3b3},
		{"sra", func(a *Asm) { a.Sra(X7, X5, X6) }, 0x4062d3b3},
		{"addi", func(a *Asm) { must(t, a.Addi(X6, X5, -2048)) }, 0x80028313},
		{"slli", func(a *Asm) { must(t, a.Slli(X6, X5, 31)) }, 0x01f29313},
		{"srli", func(a *Asm) { must(t, a.Srli(X6, X5, 31)) }, 0x01f2d313},
		{"srai", func(a *Asm) { must(t, a.Srai(X6, X5, 31)) }, 0x41f2d313},
		{"mul", func(a *Asm) { a.Mul(X7, X6, X5) }, 0x025303b3},
		{"mulhu", func(a *Asm) { a.Mulhu(X7, X6, X5) }, 0x025333b3},
		{"div", func(a *Asm) { a.Div(X7, X6, X5) }, 0x025343b3},
		{"remu", func(a *Asm) { a.Remu(X7, X6, X5) }, 0x025373b3},
		{"lw", func(a *Asm) { must(t, a.Lw(X6, X5, 4)) }, 0x0042a303},
		{"lhu", func(a *Asm) { must(t, a.Lhu(X6, X5, 4)) }, 0x0042d303},
		{"sw", func(a *Asm) { must(t, a.Sw(X5, X6, 4)) }, 0x00532223},
		{"ret", func(a *Asm) { a.Ret() }, 0x00008067},
		{"fence.i", func(a *Asm) { a.FenceI() }, 0x0000100f},
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

func TestRV32ImmediateRangeFailuresDoNotEmit(t *testing.T) {
	checks := []func(*Asm) bool{
		func(a *Asm) bool { return a.Addi(X1, X2, 2048) },
		func(a *Asm) bool { return a.Slli(X1, X2, 32) },
		func(a *Asm) bool { return a.Lw(X1, X2, -2049) },
		func(a *Asm) bool { return a.Sw(X1, X2, 2048) },
		func(a *Asm) bool { return a.Jalr(X1, X2, 2048) },
	}
	for i, check := range checks {
		var a Asm
		if check(&a) || len(a.B) != 0 {
			t.Fatalf("check %d accepted or emitted", i)
		}
	}
}

func TestRV32MovImmRoundTrip(t *testing.T) {
	values := []uint32{0, 1, 0x7ff, 0x800, 0xfffff800, 0x7fffffff, 0x80000000, 0xffffffff, 0x12345678}
	rng := rand.New(rand.NewSource(1))
	for range 10_000 {
		values = append(values, rng.Uint32())
	}
	for _, want := range values {
		var a Asm
		a.MovImm32(X7, want)
		if got := executeImmediate(a.B, X7); got != want {
			t.Fatalf("%#x reconstructed as %#x", want, got)
		}
		if n := len(a.B) / 4; n < 1 || n > 2 {
			t.Fatalf("%#x used %d instructions", want, n)
		}
	}
}

func executeImmediate(code []byte, target Reg) uint32 {
	var regs [32]uint32
	for pc := 0; pc < len(code); pc += 4 {
		w := uint32(code[pc]) | uint32(code[pc+1])<<8 | uint32(code[pc+2])<<16 | uint32(code[pc+3])<<24
		op, rd := w&0x7f, Reg(w>>7&31)
		switch op {
		case 0x37:
			regs[rd] = w & 0xfffff000
		case 0x13:
			imm := int32(w) >> 20
			regs[rd] = regs[Reg(w>>15&31)] + uint32(imm)
		default:
			panic("unexpected immediate sequence")
		}
		regs[Zero] = 0
	}
	return regs[target]
}

func TestRV32ControlPatching(t *testing.T) {
	var a Asm
	at := a.Bcond(X5, X6, CondLT)
	if !a.PatchBranch13(at, at+8) || a.wordAt(at) != 0x0062c463 {
		t.Fatalf("branch = %#08x", a.wordAt(at))
	}
	jat := a.Jal(X5)
	if !a.PatchJAL21(jat, jat-8) || a.wordAt(jat) != 0xff9ff2ef {
		t.Fatalf("jal = %#08x", a.wordAt(jat))
	}

	var far Asm
	fat := far.FarBcond(X5, X6, CondLTU, T6)
	if len(far.B) != 12 || !far.PatchFarBranch(fat, 0x123456) {
		t.Fatal("far branch rejected")
	}
	pair := fat + 4
	hi := int64(int32(far.wordAt(pair) & 0xfffff000))
	lo := int64(int32(far.wordAt(pair+4)) >> 20)
	if got := int64(pair) + hi + lo; got != 0x123456 {
		t.Fatalf("target reconstructed as %#x", got)
	}
}

func TestRV32AtomicAndCSR(t *testing.T) {
	var a Asm
	a.Lr32(X6, X5, OrderRelaxed)
	if got := word(&a); got != 0x1002a32f {
		t.Fatalf("lr.w = %#08x", got)
	}
	a.B = nil
	a.AmoMaxu32(X7, X6, X5, OrderAcquireRelease)
	if got := word(&a); got != 0xe65323af {
		t.Fatalf("amomaxu.w = %#08x", got)
	}
	a.B = nil
	a.Csrrs(X5, 0xc00, Zero)
	if got := word(&a); got != 0xc00022f3 {
		t.Fatalf("csrrs = %#08x", got)
	}
}

func TestRV32MultiwordSequences(t *testing.T) {
	var add Asm
	add.Add64(X10, X11, X12, X13, X14, X15, X16)
	if len(add.B) != 16 {
		t.Fatalf("add64 length = %d", len(add.B))
	}
	var sub Asm
	sub.Sub64(X10, X11, X12, X13, X14, X15, X16)
	if len(sub.B) != 16 {
		t.Fatalf("sub64 length = %d", len(sub.B))
	}
	var mul Asm
	mul.Mul64(X10, X11, X12, X13, X14, X15, X16, X17)
	if len(mul.B) != 24 {
		t.Fatalf("mul64 length = %d", len(mul.B))
	}
	left := Quad{X5, X6, X7, X8}
	right := Quad{X9, X10, X11, X12}
	dst := Quad{X13, X14, X15, X16}
	var swar Asm
	if !swar.AddI32x4(dst, left, right) || len(swar.B) != 16 {
		t.Fatalf("i32x4.add length = %d", len(swar.B))
	}
}

func TestRV32F64BitwiseAndPackedSWAR(t *testing.T) {
	var f Asm
	f.F64Abs(X5, X6, X7)
	f.F64Neg(X5, X6, X7)
	f.F64Copysign(X5, X6, X8, X9, X10, X11, X12)
	if len(f.B) == 0 {
		t.Fatal("f64 bitwise lowering emitted no code")
	}
	left, right := Quad{X5, X6, X7, X8}, Quad{X9, X10, X11, X12}
	var add Asm
	if !add.PackedAddSub(left, right, 8, false, X13, X14, X15) {
		t.Fatal("i8x16.add SWAR rejected")
	}
	if len(add.B) != 112 {
		t.Fatalf("i8x16.add SWAR length = %d, want 112", len(add.B))
	}
	var sub Asm
	if !sub.PackedAddSub(left, right, 16, true, X13, X14, X15) {
		t.Fatal("i16x8.sub SWAR rejected")
	}
	if len(sub.B) != 124 {
		t.Fatalf("i16x8.sub SWAR length = %d, want 124", len(sub.B))
	}
}

func TestRV32QEMUArithmetic(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	var a Asm
	a.MovImm32(A1, 6)
	a.MovImm32(A2, 7)
	a.Mul(A0, A1, A2)
	a.MovImm32(A7, 93) // Linux RISC-V __NR_exit.
	a.Ecall()
	path := filepath.Join(t.TempDir(), "rv32.elf")
	if err := os.WriteFile(path, rv32LinuxELF(a.B), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(qemu, path)
	err = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 42 {
		out, _ := cmd.CombinedOutput()
		t.Fatalf("qemu result %v, output %q", err, out)
	}
}

func rv32LinuxELF(code []byte) []byte {
	const (
		codeOff = 0x1000
		base    = 0x10000
	)
	buf := bytes.NewBuffer(make([]byte, 0, codeOff+len(code)))
	ident := [16]byte{0x7f, 'E', 'L', 'F', 1, 1, 1}
	buf.Write(ident[:])
	write := func(v any) { _ = binary.Write(buf, binary.LittleEndian, v) }
	write(uint16(2))   // ET_EXEC
	write(uint16(243)) // EM_RISCV
	write(uint32(1))
	write(uint32(base))
	write(uint32(52))
	write(uint32(0))
	write(uint32(0)) // baseline RV32I, no compressed/float flags
	write(uint16(52))
	write(uint16(32))
	write(uint16(1))
	write(uint16(0))
	write(uint16(0))
	write(uint16(0))
	write(uint32(1))
	write(uint32(codeOff))
	write(uint32(base))
	write(uint32(base))
	write(uint32(len(code)))
	write(uint32(len(code)))
	write(uint32(5))
	write(uint32(0x1000))
	for buf.Len() < codeOff {
		buf.WriteByte(0)
	}
	buf.Write(code)
	return buf.Bytes()
}
