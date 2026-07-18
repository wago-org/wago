package arm32

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func halves(a *Asm) (uint16, uint16) {
	if len(a.B) != 4 {
		panic("expected one wide instruction")
	}
	return a.halfAt(0), a.halfAt(2)
}
func must(t *testing.T, ok bool) {
	t.Helper()
	if !ok {
		t.Fatal("encoding rejected")
	}
}

func TestThumb2IntegerEncodings(t *testing.T) {
	cases := []struct {
		name          string
		emit          func(*Asm)
		first, second uint16
	}{
		{"movw", func(a *Asm) { must(t, a.Movw(R0, 0x1234)) }, 0xf241, 0x2034},
		{"movt", func(a *Asm) { must(t, a.Movt(R0, 0x1234)) }, 0xf2c1, 0x2034},
		{"mov", func(a *Asm) { must(t, a.MovReg(R3, R8)) }, 0xea4f, 0x0308},
		{"add", func(a *Asm) { must(t, a.Add(R0, R1, R2)) }, 0xeb01, 0x0002},
		{"sub", func(a *Asm) { must(t, a.Sub(R3, R4, R5)) }, 0xeba4, 0x0305},
		{"adc", func(a *Asm) { must(t, a.Adc(R3, R4, R5)) }, 0xeb44, 0x0305},
		{"sbc", func(a *Asm) { must(t, a.Sbc(R3, R4, R5)) }, 0xeb64, 0x0305},
		{"and", func(a *Asm) { must(t, a.And(R6, R7, R8)) }, 0xea07, 0x0608},
		{"orr", func(a *Asm) { must(t, a.Orr(R9, R10, R11)) }, 0xea4a, 0x090b},
		{"eor", func(a *Asm) { must(t, a.Eor(R0, R1, R2)) }, 0xea81, 0x0002},
		{"lsl-reg", func(a *Asm) { must(t, a.Lsl(R3, R4, R5)) }, 0xfa04, 0xf305},
		{"lsr-reg", func(a *Asm) { must(t, a.Lsr(R6, R7, R8)) }, 0xfa27, 0xf608},
		{"asr-reg", func(a *Asm) { must(t, a.Asr(R9, R10, R11)) }, 0xfa4a, 0xf90b},
		{"mul", func(a *Asm) { must(t, a.Mul(R0, R1, R2)) }, 0xfb01, 0xf002},
		{"umull", func(a *Asm) { must(t, a.Umull(R0, R1, R2, R3)) }, 0xfba2, 0x0103},
		{"smull", func(a *Asm) { must(t, a.Smull(R0, R1, R2, R3)) }, 0xfb82, 0x0103},
		{"udiv", func(a *Asm) { must(t, a.Udiv(R0, R1, R2)) }, 0xfbb1, 0xf0f2},
		{"sdiv", func(a *Asm) { must(t, a.Sdiv(R0, R1, R2)) }, 0xfb91, 0xf0f2},
		{"cmp", func(a *Asm) { must(t, a.Cmp(R1, R2)) }, 0xebb1, 0x0f02},
		{"ldr", func(a *Asm) { must(t, a.Ldr(R0, R1, 4)) }, 0xf8d1, 0x0004},
		{"str", func(a *Asm) { must(t, a.Str(R2, R3, 8)) }, 0xf8c3, 0x2008},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			first, second := halves(&a)
			if first != tc.first || second != tc.second {
				t.Fatalf("got %04x %04x, want %04x %04x", first, second, tc.first, tc.second)
			}
		})
	}
}

func TestThumb2ImmediateAndRegisterValidation(t *testing.T) {
	checks := []func(*Asm) bool{
		func(a *Asm) bool { return a.Movw(PC, 0) },
		func(a *Asm) bool { return a.Add(PC, R0, R1) },
		func(a *Asm) bool { return a.LslImm(R0, R1, 32) },
		func(a *Asm) bool { return a.LsrImm(R0, R1, 0) },
		func(a *Asm) bool { return a.RorImm(R0, R1, 0) },
		func(a *Asm) bool { return a.Ldr(R0, R1, 4096) },
		func(a *Asm) bool { return a.Umull(R0, R0, R1, R2) },
	}
	for i, check := range checks {
		var a Asm
		if check(&a) || len(a.B) != 0 {
			t.Fatalf("check %d accepted or emitted", i)
		}
	}
}

func TestThumb2MultiwordAndSWARSequences(t *testing.T) {
	var add Asm
	must(t, add.Add64(R0, R1, R2, R3, R4, R5))
	if len(add.B) != 8 {
		t.Fatalf("add64 length = %d", len(add.B))
	}
	var sub Asm
	must(t, sub.Sub64(R0, R1, R2, R3, R4, R5))
	if len(sub.B) != 8 {
		t.Fatalf("sub64 length = %d", len(sub.B))
	}
	var mul Asm
	must(t, mul.Mul64(R0, R1, R2, R3, R4, R5, R6))
	if len(mul.B) != 20 {
		t.Fatalf("mul64 length = %d", len(mul.B))
	}
	left := Quad{R0, R1, R2, R3}
	right := Quad{R4, R5, R6, R7}
	dst := Quad{R8, R9, R10, R11}
	var swar Asm
	must(t, swar.AddI32x4(dst, left, right))
	if len(swar.B) != 16 {
		t.Fatalf("i32x4.add length = %d", len(swar.B))
	}
}

func TestThumb2F64BitwiseAndPackedSWAR(t *testing.T) {
	var f Asm
	must(t, f.F64Abs(R0, R1, R2))
	must(t, f.F64Neg(R0, R1, R2))
	must(t, f.F64Copysign(R0, R1, R3, R4, R5, R6, R7))
	if len(f.B) == 0 {
		t.Fatal("f64 bitwise lowering emitted no code")
	}
	left, right := Quad{R0, R1, R2, R3}, Quad{R4, R5, R6, R7}
	var add Asm
	must(t, add.PackedAddSub(left, right, 8, false, R8, R9, R10))
	if len(add.B) != 112 {
		t.Fatalf("i8x16.add SWAR length = %d, want 112", len(add.B))
	}
	var sub Asm
	must(t, sub.PackedAddSub(left, right, 16, true, R8, R9, R10))
	if len(sub.B) != 128 {
		t.Fatalf("i16x8.sub SWAR length = %d, want 128", len(sub.B))
	}
}

func TestThumb2BranchPatching(t *testing.T) {
	for _, target := range []int{0, 2, 4, 0x123456, -0x123456} {
		var a Asm
		at := a.Branch()
		if !a.PatchBranch(at, target) {
			t.Fatalf("target %#x rejected", target)
		}
		if got := decodeBranchTarget(a.halfAt(at), a.halfAt(at+2), at); got != target {
			t.Fatalf("target %#x decoded as %#x", target, got)
		}
	}
	var far Asm
	at := far.FarBcond(CondLT)
	if len(far.B) != 6 {
		t.Fatalf("far conditional length = %d", len(far.B))
	}
	if got := far.halfAt(at); got != 0xda01 { // GE skips the following B.W.
		t.Fatalf("inverse skip = %04x", got)
	}
	if !far.PatchFarBranch(at, 0x123456) {
		t.Fatal("far conditional patch rejected")
	}
}

func decodeBranchTarget(first, second uint16, at int) int {
	s := uint32(first>>10) & 1
	imm10 := uint32(first & 0x3ff)
	j1, j2 := uint32(second>>13)&1, uint32(second>>11)&1
	i1, i2 := (^uint32(0)^j1^s)&1, (^uint32(0)^j2^s)&1
	imm11 := uint32(second & 0x7ff)
	u := s<<24 | i1<<23 | i2<<22 | imm10<<12 | imm11<<1
	if s != 0 {
		u |= 0xfe000000
	}
	return at + 4 + int(int32(u))
}

func TestThumb2QEMUArithmetic(t *testing.T) {
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	var a Asm
	must(t, a.MovImm32(R1, 6))
	must(t, a.MovImm32(R2, 7))
	must(t, a.Mul(R0, R1, R2))
	must(t, a.MovImm32(R7, 1)) // Linux ARM __NR_exit.
	a.Svc(0)

	path := filepath.Join(t.TempDir(), "thumb.elf")
	if err := os.WriteFile(path, thumbLinuxELF(a.B), 0o755); err != nil {
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

func thumbLinuxELF(code []byte) []byte {
	const (
		codeOff = 0x1000
		base    = 0x10000
	)
	buf := bytes.NewBuffer(make([]byte, 0, codeOff+len(code)))
	ident := [16]byte{0x7f, 'E', 'L', 'F', 1, 1, 1}
	buf.Write(ident[:])
	write := func(v any) { _ = binary.Write(buf, binary.LittleEndian, v) }
	write(uint16(2))          // ET_EXEC
	write(uint16(40))         // EM_ARM
	write(uint32(1))          // EV_CURRENT
	write(uint32(base | 1))   // Thumb entry
	write(uint32(52))         // program header offset
	write(uint32(0))          // section header offset
	write(uint32(0x05000200)) // EABI5, soft-float
	write(uint16(52))
	write(uint16(32))
	write(uint16(1))
	write(uint16(0))
	write(uint16(0))
	write(uint16(0))
	write(uint32(1)) // PT_LOAD
	write(uint32(codeOff))
	write(uint32(base))
	write(uint32(base))
	write(uint32(len(code)))
	write(uint32(len(code)))
	write(uint32(5)) // PF_R|PF_X
	write(uint32(0x1000))
	for buf.Len() < codeOff {
		buf.WriteByte(0)
	}
	buf.Write(code)
	return buf.Bytes()
}
