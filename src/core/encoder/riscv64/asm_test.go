package riscv64

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// These words were independently assembled and disassembled with LLVM 19.
// testdata/scalar.s has the same order, and TestAssembler compares live bytes.
var encodingCases = []struct {
	name string
	emit func(*Asm)
	want uint32
}{
	{"add", func(a *Asm) { a.Add(X5, X6, X7) }, 0x007302b3},
	{"sub", func(a *Asm) { a.Sub(X5, X6, X7) }, 0x407302b3},
	{"mul", func(a *Asm) { a.Mul(X5, X6, X7) }, 0x027302b3},
	{"mulh", func(a *Asm) { a.Mulh(X5, X6, X7) }, 0x027312b3},
	{"mulhu", func(a *Asm) { a.Mulhu(X5, X6, X7) }, 0x027332b3},
	{"and", func(a *Asm) { a.And(X5, X6, X7) }, 0x007372b3},
	{"or", func(a *Asm) { a.Or(X5, X6, X7) }, 0x007362b3},
	{"xor", func(a *Asm) { a.Xor(X5, X6, X7) }, 0x007342b3},
	{"slt", func(a *Asm) { a.Slt(X5, X6, X7) }, 0x007322b3},
	{"sltu", func(a *Asm) { a.Sltu(X5, X6, X7) }, 0x007332b3},
	{"sll", func(a *Asm) { a.Sll(X5, X6, X7) }, 0x007312b3},
	{"srl", func(a *Asm) { a.Srl(X5, X6, X7) }, 0x007352b3},
	{"sra", func(a *Asm) { a.Sra(X5, X6, X7) }, 0x407352b3},
	{"addw", func(a *Asm) { a.Addw(X5, X6, X7) }, 0x007302bb},
	{"subw", func(a *Asm) { a.Subw(X5, X6, X7) }, 0x407302bb},
	{"mulw", func(a *Asm) { a.Mulw(X5, X6, X7) }, 0x027302bb},
	{"sllw", func(a *Asm) { a.Sllw(X5, X6, X7) }, 0x007312bb},
	{"srlw", func(a *Asm) { a.Srlw(X5, X6, X7) }, 0x007352bb},
	{"sraw", func(a *Asm) { a.Sraw(X5, X6, X7) }, 0x407352bb},
	{"addi min", func(a *Asm) { a.Addi(X5, X6, -2048) }, 0x80030293},
	{"addi max/high registers", func(a *Asm) { a.Addi(X31, X30, 2047) }, 0x7fff0f93},
	{"addiw", func(a *Asm) { a.Addiw(X5, X6, -1) }, 0xfff3029b},
	{"xori", func(a *Asm) { a.Xori(X5, X6, -1) }, 0xfff34293},
	{"sltiu", func(a *Asm) { a.Sltiu(X5, X6, 1) }, 0x00133293},
	{"lui sign bit", func(a *Asm) { a.Lui(X5, 0x80000) }, 0x800002b7},
	{"auipc", func(a *Asm) { a.Auipc(X31, 0x7ffff) }, 0x7fffff97},
	{"slli", func(a *Asm) { a.Slli(X5, X6, 63) }, 0x03f31293},
	{"srli", func(a *Asm) { a.Srli(X5, X6, 32) }, 0x02035293},
	{"srai", func(a *Asm) { a.Srai(X5, X6, 63) }, 0x43f35293},
	{"slliw", func(a *Asm) { a.Slliw(X5, X6, 31) }, 0x01f3129b},
	{"srliw", func(a *Asm) { a.Srliw(X5, X6, 31) }, 0x01f3529b},
	{"sraiw", func(a *Asm) { a.Sraiw(X5, X6, 31) }, 0x41f3529b},
	{"ld", func(a *Asm) { a.Ld(X5, X6, -2048) }, 0x80033283},
	{"lwu", func(a *Asm) { a.Lwu(X5, X6, 2047) }, 0x7ff36283},
	{"lw", func(a *Asm) { a.Lw(X5, X6, -1) }, 0xfff32283},
	{"sd", func(a *Asm) { a.Sd(X5, X6, -2048) }, 0x80533023},
	{"sw/high registers", func(a *Asm) { a.Sw(X31, X30, 2047) }, 0x7fff2fa3},
	{"jalr", func(a *Asm) { a.Jalr(RA, X31, -2048) }, 0x800f80e7},
	{"ret", func(a *Asm) { a.Ret() }, 0x00008067},
	{"nop", func(a *Asm) { a.Nop() }, 0x00000013},
	{"beq", func(a *Asm) { a.Branch(EQ, X5, X6, 12) }, 0x00628663},
	{"bne min", func(a *Asm) { a.Branch(NE, X5, X6, -4096) }, 0x80629063},
	{"blt max", func(a *Asm) { a.Branch(LT, X5, X6, 4092) }, 0x7e62cee3},
	{"bge negative", func(a *Asm) { a.Branch(GE, X5, X6, -4) }, 0xfe62dee3},
	{"bltu", func(a *Asm) { a.Branch(LTU, X31, X30, 4) }, 0x01efe263},
	{"bgeu", func(a *Asm) { a.Branch(GEU, X31, X30, 0) }, 0x01eff063},
	{"jal max", func(a *Asm) { a.Jal(Zero, 1048572) }, 0x7fdff06f},
	{"jal min", func(a *Asm) { a.Jal(RA, -1048576) }, 0x800000ef},
	{"div", func(a *Asm) { a.Div(X5, X6, X7) }, 0x027342b3},
	{"divu", func(a *Asm) { a.Divu(X5, X6, X7) }, 0x027352b3},
	{"rem", func(a *Asm) { a.Rem(X5, X6, X7) }, 0x027362b3},
	{"remu", func(a *Asm) { a.Remu(X5, X6, X7) }, 0x027372b3},
	{"divw", func(a *Asm) { a.Divw(X5, X6, X7) }, 0x027342bb},
	{"divuw", func(a *Asm) { a.Divuw(X5, X6, X7) }, 0x027352bb},
	{"remw", func(a *Asm) { a.Remw(X5, X6, X7) }, 0x027362bb},
	{"remuw", func(a *Asm) { a.Remuw(X5, X6, X7) }, 0x027372bb},
	{"lbu", func(a *Asm) { a.Lbu(X5, X6, 7) }, 0x00734283},
	{"sb", func(a *Asm) { a.Sb(X5, X6, 7) }, 0x005303a3},
	{"fence acquire", func(a *Asm) { a.Fence(FenceRead, FenceRead|FenceWrite) }, 0x0230000f},
	{"fence full", func(a *Asm) { a.Fence(15, 15) }, 0x0ff0000f},
}

func TestEncodings(t *testing.T) {
	for _, c := range encodingCases {
		t.Run(c.name, func(t *testing.T) {
			var a Asm
			c.emit(&a)
			if a.Err() != nil || a.Len() != 4 {
				t.Fatalf("len=%d err=%v", a.Len(), a.Err())
			}
			if got := binary.LittleEndian.Uint32(a.B); got != c.want {
				t.Fatalf("got %#08x want %#08x", got, c.want)
			}
		})
	}
}

func TestAssembler(t *testing.T) {
	want := assembledBytes(t, "scalar")
	var a Asm
	for _, c := range encodingCases {
		c.emit(&a)
	}
	if a.Err() != nil || !bytes.Equal(a.B, want) {
		t.Fatalf("assembler comparison: err=%v\nencoder %x\nclang   %x", a.Err(), a.B, want)
	}
}

func assembledBytes(t *testing.T, name string) []byte {
	t.Helper()
	// Enable this independent tool gate explicitly; ordinary package tests also
	// run on hosts without an assembler. A configured but broken tool is a failure.
	clang := os.Getenv("WAGO_RV64_CLANG")
	if clang == "" {
		t.Skip("set WAGO_RV64_CLANG to a RISC-V-capable clang for live assembler comparison")
	}
	obj := filepath.Join(t.TempDir(), name+".o")
	cmd := exec.Command(clang, "--target=riscv64-linux-gnu", "-march=rv64g", "-mabi=lp64d",
		"-c", "testdata/"+name+".s", "-o", obj)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("assembler: %v\n%s", err, out)
	}
	f, err := elf.Open(obj)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Machine != elf.EM_RISCV || f.Class != elf.ELFCLASS64 || f.Data != elf.ELFDATA2LSB {
		t.Fatal("assembler did not produce little-endian RV64 ELF")
	}
	for _, s := range f.Sections {
		if (s.Type == elf.SHT_RELA || s.Type == elf.SHT_REL) && s.Size != 0 {
			t.Fatalf("unresolved assembler relocations in %s", s.Name)
		}
	}
	section := f.Section(".text")
	if section == nil {
		t.Fatal("assembler output has no .text")
	}
	want, err := section.Data()
	if err != nil {
		t.Fatal(err)
	}
	return want
}

func TestInvalidOperandsDoNotEmit(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
	}{
		{"dst", func(a *Asm) { a.Add(32, X1, X2) }},
		{"source", func(a *Asm) { a.Sub(X1, 255, X2) }},
		{"second source", func(a *Asm) { a.Mul(X1, X2, 32) }},
		{"constant", func(a *Asm) { a.Constant32(32, 0xffffffff) }},
		{"normalization", func(a *Asm) { a.ZeroExtend32(X1, 32) }},
		{"addi overflow", func(a *Asm) { a.Addi(X1, X2, 2048) }},
		{"addi underflow", func(a *Asm) { a.Addi(X1, X2, -2049) }},
		{"upper", func(a *Asm) { a.Lui(X1, 1<<20) }},
		{"upper pc", func(a *Asm) { a.Auipc(X1, 1<<20) }},
		{"load", func(a *Asm) { a.Lwu(X1, X2, 2048) }},
		{"load base", func(a *Asm) { a.Ld(X1, 32, 0) }},
		{"store", func(a *Asm) { a.Sw(X1, X2, -2049) }},
		{"store source", func(a *Asm) { a.Sd(32, X2, 0) }},
		{"jalr", func(a *Asm) { a.Jalr(X1, X2, 2048) }},
		{"shift64", func(a *Asm) { a.Slli(X1, X2, 64) }},
		{"shift32", func(a *Asm) { a.Sraiw(X1, X2, 32) }},
		{"branch alignment", func(a *Asm) { a.Branch(EQ, X1, X2, 2) }},
		{"branch min", func(a *Asm) { a.Branch(EQ, X1, X2, -4100) }},
		{"branch max", func(a *Asm) { a.Branch(EQ, X1, X2, 4096) }},
		{"condition", func(a *Asm) { a.Branch(2, X1, X2, 0) }},
		{"long condition", func(a *Asm) { a.BranchLong(8, X1, X2) }},
		{"long source", func(a *Asm) { a.BranchLong(EQ, X1, 32) }},
		{"jal alignment", func(a *Asm) { a.Jal(X1, -2) }},
		{"jal min", func(a *Asm) { a.Jal(X1, -1048580) }},
		{"jal max", func(a *Asm) { a.Jal(X1, 1048576) }},
		{"fence predecessor", func(a *Asm) { a.Fence(16, 3) }},
		{"fence successor", func(a *Asm) { a.Fence(3, 255) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var a Asm
			a.Nop()
			before := append([]byte(nil), a.B...)
			c.emit(&a)
			if a.Err() == nil || !bytes.Equal(a.B, before) {
				t.Fatalf("invalid operand emitted: %x, %v", a.B, a.Err())
			}
			first := a.Err()
			a.Nop()
			if a.JumpLong(false) != -1 || a.Err() != first || !bytes.Equal(a.B, before) {
				t.Fatal("first error was not sticky")
			}
		})
	}
	a := Asm{B: []byte{0}}
	a.Constant64(X5, 0x7fffffffffffffff)
	if a.Err() == nil || len(a.B) != 1 {
		t.Fatal("unaligned buffer accepted")
	}
}

func TestPhysicalRegisterReservations(t *testing.T) {
	if Zero != 0 || RA != 1 || SP != 2 || GP != 3 || TP != 4 || FP != 8 || Scratch0 != 30 || Scratch1 != 31 {
		t.Fatal("aliases changed physical register IDs")
	}
	reserved := map[Reg]bool{X0: true, X1: true, X2: true, X3: true, X4: true,
		X8: true, X24: true, X25: true, X26: true, X27: true, X30: true, X31: true}
	for r := X0; r <= X31; r++ {
		if (ReservedMask&(1<<r) != 0) != reserved[r] {
			t.Errorf("reservation for physical x%d", r)
		}
		var a Asm
		a.Add(r, r, r)
		w := binary.LittleEndian.Uint32(a.B)
		if (w>>7)&31 != uint32(r) || (w>>15)&31 != uint32(r) || (w>>20)&31 != uint32(r) {
			t.Fatalf("physical x%d encoded incorrectly", r)
		}
	}
	if ReservedMask>>32 != 0 {
		t.Fatal("mask contains nonexistent physical registers")
	}
}
