//go:build wago_regalloccheck && !tinygo

package riscv64

import (
	"encoding/binary"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
)

func decodedGPWrites(t *testing.T, b []byte) []uint32 {
	t.Helper()
	var masks []uint32
	for pc := 0; pc < len(b); pc += 4 {
		w := binary.LittleEndian.Uint32(b[pc:])
		op := w & 127
		dst := (w >> 7) & 31
		switch op {
		case 0x33, 0x3b, 0x13, 0x1b, 0x37, 0x17, 0x03:
			if dst != 0 {
				masks = append(masks, 1<<dst)
			}
		case 0x67, 0x6f:
			if dst != 0 {
				masks = append(masks, ^uint32(1))
			}
		case 0x23, 0x63, 0x0f:
		case 0x53:
			// OP-FP comparisons (10100), FP-to-int conversions (11000),
			// class and FP-to-GP moves (11100) write the integer file.
			switch (w >> 27) & 31 {
			case 0x14, 0x18, 0x1c:
				if dst != 0 {
					masks = append(masks, 1<<dst)
				}
			}
		case 0x73:
			if (w>>12)&7 == 0 {
				t.Fatalf("unmodeled SYSTEM instruction %#x", w)
			}
			if dst != 0 {
				masks = append(masks, 1<<dst)
			}
		default:
			t.Fatalf("unmodeled instruction %#x", w)
		}
	}
	return masks
}

func TestGPWritesEveryTypedEmitter(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm, Reg)
	}{
		{"Add", func(a *Asm, d Reg) { a.Add(d, X10, X11) }},
		{"Sub", func(a *Asm, d Reg) { a.Sub(d, X10, X11) }},
		{"Mul", func(a *Asm, d Reg) { a.Mul(d, X10, X11) }},
		{"Div", func(a *Asm, d Reg) { a.Div(d, X10, X11) }},
		{"Divu", func(a *Asm, d Reg) { a.Divu(d, X10, X11) }},
		{"Rem", func(a *Asm, d Reg) { a.Rem(d, X10, X11) }},
		{"Remu", func(a *Asm, d Reg) { a.Remu(d, X10, X11) }},
		{"And", func(a *Asm, d Reg) { a.And(d, X10, X11) }},
		{"Or", func(a *Asm, d Reg) { a.Or(d, X10, X11) }},
		{"Xor", func(a *Asm, d Reg) { a.Xor(d, X10, X11) }},
		{"Slt", func(a *Asm, d Reg) { a.Slt(d, X10, X11) }},
		{"Sltu", func(a *Asm, d Reg) { a.Sltu(d, X10, X11) }},
		{"Sll", func(a *Asm, d Reg) { a.Sll(d, X10, X11) }},
		{"Srl", func(a *Asm, d Reg) { a.Srl(d, X10, X11) }},
		{"Sra", func(a *Asm, d Reg) { a.Sra(d, X10, X11) }},
		{"Addw", func(a *Asm, d Reg) { a.Addw(d, X10, X11) }},
		{"Subw", func(a *Asm, d Reg) { a.Subw(d, X10, X11) }},
		{"Mulw", func(a *Asm, d Reg) { a.Mulw(d, X10, X11) }},
		{"Divw", func(a *Asm, d Reg) { a.Divw(d, X10, X11) }},
		{"Divuw", func(a *Asm, d Reg) { a.Divuw(d, X10, X11) }},
		{"Remw", func(a *Asm, d Reg) { a.Remw(d, X10, X11) }},
		{"Remuw", func(a *Asm, d Reg) { a.Remuw(d, X10, X11) }},
		{"Sllw", func(a *Asm, d Reg) { a.Sllw(d, X10, X11) }},
		{"Srlw", func(a *Asm, d Reg) { a.Srlw(d, X10, X11) }},
		{"Sraw", func(a *Asm, d Reg) { a.Sraw(d, X10, X11) }},
		{"Addi", func(a *Asm, d Reg) { a.Addi(d, X10, 7) }},
		{"Addiw", func(a *Asm, d Reg) { a.Addiw(d, X10, 7) }},
		{"Xori", func(a *Asm, d Reg) { a.Xori(d, X10, 7) }},
		{"Sltiu", func(a *Asm, d Reg) { a.Sltiu(d, X10, 7) }},
		{"Slli", func(a *Asm, d Reg) { a.Slli(d, X10, 7) }},
		{"Srli", func(a *Asm, d Reg) { a.Srli(d, X10, 7) }},
		{"Srai", func(a *Asm, d Reg) { a.Srai(d, X10, 7) }},
		{"Slliw", func(a *Asm, d Reg) { a.Slliw(d, X10, 7) }},
		{"Srliw", func(a *Asm, d Reg) { a.Srliw(d, X10, 7) }},
		{"Sraiw", func(a *Asm, d Reg) { a.Sraiw(d, X10, 7) }},
		{"Lui", func(a *Asm, d Reg) { a.Lui(d, 7) }},
		{"Auipc", func(a *Asm, d Reg) { a.Auipc(d, 7) }},
		{"Jal", func(a *Asm, d Reg) { a.Jal(d, 8) }},
		{"Ld", func(a *Asm, d Reg) { a.Ld(d, SP, 8) }},
		{"Lw", func(a *Asm, d Reg) { a.Lw(d, SP, 8) }},
		{"Lwu", func(a *Asm, d Reg) { a.Lwu(d, SP, 8) }},
		{"Lbu", func(a *Asm, d Reg) { a.Lbu(d, SP, 8) }},
		{"Jalr", func(a *Asm, d Reg) { a.Jalr(d, SP, 8) }},
		{"Sd", func(a *Asm, d Reg) { a.Sd(d, SP, 8) }},
		{"Sw", func(a *Asm, d Reg) { a.Sw(d, SP, 8) }},
		{"Sb", func(a *Asm, d Reg) { a.Sb(d, SP, 8) }},
		{"ZeroExtend32", func(a *Asm, d Reg) { a.ZeroExtend32(d, X10) }},
		{"Constant32", func(a *Asm, d Reg) { a.Constant32(d, 0xdeadbeef) }},
		{"Constant64", func(a *Asm, d Reg) { a.Constant64(d, 0x7fedcba987654321) }},
		{"Branch", func(a *Asm, d Reg) { a.Branch(EQ, d, X10, 4) }},
		{"BranchLong", func(a *Asm, d Reg) { a.BranchLong(LTU, d, X10) }},
		{"JumpLong", func(a *Asm, d Reg) { a.JumpLong(d != Zero) }},
		{"Ret", func(a *Asm, d Reg) { a.Ret() }},
		{"Nop", func(a *Asm, d Reg) { a.Nop() }},
		{"Fence", func(a *Asm, d Reg) { a.Fence(FenceRead, FenceRead|FenceWrite) }},
	}
	for _, fc := range floatEncodingCases {
		fc := fc
		cases = append(cases, struct {
			name string
			emit func(*Asm, Reg)
		}{
			fc.name, func(a *Asm, d Reg) { fc.emit(a, d, FReg(d)) },
		})
	}
	audited := map[string]bool{"Err": true, "Len": true, "ObserveGPWrites": true, "ObserveRegalloc": true, "PatchLong": true}
	for _, tc := range cases {
		audited[tc.name] = true
		for _, d := range []Reg{Zero, X9, SP, X31} {
			t.Run(tc.name+"/"+string(rune('a'+d)), func(t *testing.T) {
				var a Asm
				var seen []uint32
				a.ObserveGPWrites(func(mask uint32) { seen = append(seen, mask) })
				tc.emit(&a, d)
				if a.Err() != nil {
					t.Fatal(a.Err())
				}
				if want := decodedGPWrites(t, a.B); !reflect.DeepEqual(seen, want) {
					t.Fatalf("seen %x want %x", seen, want)
				}
			})
		}
	}
	// A new public typed emitter must receive its own independent decoder control.
	for _, path := range []string{"asm.go", "branch.go", "constant.go", "float.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			f, ok := decl.(*ast.FuncDecl)
			if !ok || f.Recv == nil || !f.Name.IsExported() {
				continue
			}
			if !audited[f.Name.Name] {
				t.Fatalf("typed emitter %s lacks coverage", f.Name.Name)
			}
		}
	}
}

func TestGPWritesInvalidAndStickyEmission(t *testing.T) {
	for _, bad := range []func(*Asm){
		func(a *Asm) { a.Add(Reg(32), X5, X6) }, func(a *Asm) { a.Addi(X9, X5, 2048) },
		func(a *Asm) { a.Lui(X9, 1<<20) }, func(a *Asm) { a.Slli(X9, X5, 64) },
		func(a *Asm) { a.Jal(X9, 2) }, func(a *Asm) { a.Jalr(X9, X5, -2049) },
		func(a *Asm) { a.Ld(X9, Reg(32), 0) }, func(a *Asm) { a.Sd(X5, SP, 2048) },
	} {
		var a Asm
		a.ObserveGPWrites(func(uint32) { t.Fatal("invalid/sticky physical effect") })
		a.ObserveRegalloc(func(regalloccheck.Effect) { t.Fatal("invalid/sticky transfer effect") })
		bad(&a)
		if a.Err() == nil {
			t.Fatal("missing invalid operand error")
		}
		a.Constant64(X9, 42)
		a.JumpLong(true)
		a.Sd(X5, SP, 0)
		if len(a.B) != 0 {
			t.Fatal("sticky error emitted bytes")
		}
	}
	for _, emit := range []func(*Asm){func(a *Asm) { a.Add(X9, X5, X6) }, func(a *Asm) { a.Ld(X9, SP, 0) }, func(a *Asm) { a.Lui(X9, 1) }, func(a *Asm) { a.Jal(X9, 4) }, func(a *Asm) { a.JumpLong(true) }} {
		a := Asm{B: []byte{1}}
		a.ObserveGPWrites(func(uint32) { t.Fatal("unaligned physical effect") })
		emit(&a)
		if a.Err() == nil || len(a.B) != 1 {
			t.Fatal("alignment validation")
		}
	}
}

func TestGPWritesPatchIsNotAnotherInstruction(t *testing.T) {
	var a Asm
	n := 0
	a.ObserveGPWrites(func(uint32) { n++ })
	site := a.JumpLong(true)
	before := n
	if err := a.PatchLong(site, 64); err != nil {
		t.Fatal(err)
	}
	if n != before {
		t.Fatal("patch duplicated physical effects")
	}
}

func TestRegallocTransfersWidthsAndIndependentScopes(t *testing.T) {
	var a Asm
	var state regalloccheck.State
	r := regalloccheck.Register(regalloccheck.GP, 9)
	value := state.Seed(r, 8)
	fp := regalloccheck.Register(regalloccheck.FP, 9)
	fpValue := state.Seed(fp, 8)
	a.ObserveRegalloc(state.Apply)
	a.Sd(X9, SP, 16)
	a.Ld(X10, SP, 16)
	state.Expect("actual stack reload", regalloccheck.Register(regalloccheck.GP, 10), value)
	a.Addi(X11, X10, 0)
	state.Expect("actual register move", regalloccheck.Register(regalloccheck.GP, 11), value)
	a.Lwu(X9, SP, 16)
	state.Expect("low word", r, state.Read(regalloccheck.Slot(16), 4))
	high := state.Read(r, 8)
	if !reflect.DeepEqual(high[4:8], make(regalloccheck.Value, 4)) {
		t.Fatal("old upper byte survived word load")
	}
	state.Expect("separate FP bank", fp, fpValue)
	a.Addiw(X9, X9, 0)
	if !reflect.DeepEqual(state.Read(r, 8), make(regalloccheck.Value, 8)) {
		t.Fatal("W definition retained input identity")
	}
	n := 0
	a.ObserveGPWrites(func(uint32) { n++ })
	outer := a.ObserveRegalloc(func(regalloccheck.Effect) {})
	inner := a.ObserveRegalloc(func(regalloccheck.Effect) {})
	a.Add(X9, X10, X11)
	a.ObserveRegalloc(inner)
	a.Add(X9, X10, X11)
	a.ObserveRegalloc(outer)
	if n != 2 {
		t.Fatal("transfer scope hid GP writes")
	}
	a.Jal(X5, 4)
	// Physical calls conservatively kill both banks in symbolic windows too.
	if !reflect.DeepEqual(state.Read(fp, 8), make(regalloccheck.Value, 8)) {
		t.Fatal("call retained FP identity")
	}
}

func TestFloatEffectsValidateBeforeObservation(t *testing.T) {
	for _, bad := range invalidFloatEmitters() {
		var a Asm
		a.ObserveGPWrites(func(uint32) { t.Fatal("invalid float physical effect") })
		a.ObserveRegalloc(func(regalloccheck.Effect) { t.Fatal("invalid float symbolic effect") })
		bad(&a)
		for _, c := range floatEncodingCases {
			c.emit(&a, X5, F5)
		}
		if a.Err() == nil || len(a.B) != 0 {
			t.Fatal("invalid/sticky float emission")
		}
	}
	for _, c := range floatEncodingCases {
		a := Asm{B: []byte{1}}
		a.ObserveGPWrites(func(uint32) { t.Fatal("unaligned float physical effect") })
		a.ObserveRegalloc(func(regalloccheck.Effect) { t.Fatal("unaligned float symbolic effect") })
		c.emit(&a, X5, F5)
		if a.Err() == nil || len(a.B) != 1 {
			t.Fatalf("unaligned %s emitted", c.name)
		}
	}
}

func TestFloatDefinitionsKillOnlyTheirRegisterFile(t *testing.T) {
	for _, emit := range []func(*Asm){
		func(a *Asm) { a.FaddS(F9, F10, F11, RNE) },
		func(a *Asm) { a.FmvWX(F9, X10) },
		func(a *Asm) { a.FcvtDL(F9, X10, RNE) },
	} {
		var a Asm
		var state regalloccheck.State
		gp9 := regalloccheck.Register(regalloccheck.GP, 9)
		fp9 := regalloccheck.Register(regalloccheck.FP, 9)
		gpValue := state.Seed(gp9, 8)
		state.Seed(fp9, 8)
		a.ObserveRegalloc(state.Apply)
		a.ObserveGPWrites(func(uint32) { t.Fatal("FP definition reported GP write") })
		emit(&a)
		state.Expect("same-numbered GP survives FP definition", gp9, gpValue)
		if !reflect.DeepEqual(state.Read(fp9, 8), make(regalloccheck.Value, 8)) {
			t.Fatal("FP definition retained old FLEN identity")
		}
	}
	var a Asm
	var state regalloccheck.State
	fp9 := regalloccheck.Register(regalloccheck.FP, 9)
	fpValue := state.Seed(fp9, 8)
	state.Seed(regalloccheck.Register(regalloccheck.GP, 9), 8)
	a.ObserveRegalloc(state.Apply)
	a.FmvXW(X9, F9)
	state.Expect("GP definition preserves FP source", fp9, fpValue)
	if !reflect.DeepEqual(state.Read(regalloccheck.Register(regalloccheck.GP, 9), 8), make(regalloccheck.Value, 8)) {
		t.Fatal("FP-to-GP definition retained GP identity")
	}
	// Unlike x0, f0 is writable. Both ends of the FP bank have full FLEN
	// identities even when the newly defined value is single precision.
	for _, dst := range []FReg{F0, F31} {
		var a Asm
		var state regalloccheck.State
		f := regalloccheck.Register(regalloccheck.FP, uint8(dst))
		g := regalloccheck.Register(regalloccheck.GP, uint8(dst))
		state.Seed(f, 8)
		value := state.Seed(g, 8)
		a.ObserveRegalloc(state.Apply)
		a.ObserveGPWrites(func(uint32) { t.Fatal("single-precision definition wrote GP") })
		a.FaddS(dst, F6, F7, RNE)
		state.Expect("GP survives FP edge-register definition", g, value)
		if !reflect.DeepEqual(state.Read(f, 8), make(regalloccheck.Value, 8)) {
			t.Fatal("single-precision FP definition retained upper identity")
		}
	}
}
