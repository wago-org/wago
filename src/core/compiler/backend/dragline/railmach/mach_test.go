package railmach

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func machineModule(params, results []wasm.ValType, body []byte) *wasm.Module {
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results)))
	funcSec := wasmtest.Section(3, wasmtest.Vec([]byte{0}))
	codeSec := wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body)))
	m, err := wasm.DecodeModule(wasmtest.Module(typeSec, funcSec, codeSec))
	if err != nil {
		panic(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		panic(err)
	}
	return m
}

func buildMachineTest(t *testing.T, target Target, m *wasm.Module) *Func {
	t.Helper()
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := railssa.BuildCFG(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	locals, err := railssa.BuildLocalSSA(stack, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := railssa.BuildValueFlow(stack, cfg, locals, nil)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := railssa.BuildSemanticFunc(stack, cfg, flow, nil)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := Build(target, cfg, flow, semantic, nil)
	if err != nil {
		t.Fatal(err)
	}
	return machine
}

func TestBuildMarksARM64DefaultFloatLocalRematerializable(t *testing.T) {
	function := []byte{0x01, 0x01, 0x7c, 0x20, 0x00, 0x0b} // one f64 local; local.get 0
	code := append(wasmtest.ULEB(uint32(len(function))), function...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	f := buildMachineTest(t, TargetARM64, m)
	for _, data := range f.VRegs {
		if data.Flags&VRegInitial != 0 && data.InitialLocal == 0 {
			if data.Type != TypeF64 || data.Flags&VRegRematerializable == 0 {
				t.Fatalf("default f64 local = %#v", data)
			}
			return
		}
	}
	t.Fatal("default f64 local is unavailable")
}

func TestSemanticOpcodeTablesCoverSelectedRanges(t *testing.T) {
	for _, selectedRange := range [][2]MOpcode{
		{OpAMD64V128Move, opAMD64SelectedEnd},
		{OpARM64V128Move, opARM64SelectedEnd},
	} {
		for op := selectedRange[0]; op < selectedRange[1]; op++ {
			want := semanticOpcodeSlow(op)
			if got := SemanticOpcode(op); got != want {
				t.Fatalf("SemanticOpcode(%d) = %d, want %d", uint16(op), uint16(got), uint16(want))
			}
		}
	}
	if got := SemanticOpcode(wasm.InstrI64Add); got != wasm.InstrI64Add {
		t.Fatalf("generic semantic opcode changed: got %d", uint16(got))
	}
	for _, op := range []MOpcode{OpAMD64I8x16Bitmask, OpARM64I8x16Bitmask} {
		if got := SemanticOpcode(op); got != wasm.InstrI8x16Bitmask {
			t.Fatalf("selected bitmask semantic opcode = %d, want %d", uint16(got), uint16(wasm.InstrI8x16Bitmask))
		}
	}
}

func TestDenseRecordSizes(t *testing.T) {
	if got := unsafe.Sizeof(Inst{}); got != 24 {
		t.Fatalf("Inst size = %d, want 24", got)
	}
	if got := unsafe.Sizeof(Operand{}); got != 12 {
		t.Fatalf("Operand size = %d, want 12", got)
	}
}

func TestSelectedOpcodeNamespaceIsTargetSpecific(t *testing.T) {
	if IsSelectedOpcode(wasm.InstrV128Load) || !IsSelectedOpcode(OpAMD64V128Load) || !IsSelectedOpcode(OpARM64V128Load) {
		t.Fatal("selected opcode namespace overlaps generic Wasm operations")
	}
	if SelectedOpcodeTarget(OpAMD64V128Load) != TargetAMD64 || SelectedOpcodeTarget(OpARM64V128Load) != TargetARM64 {
		t.Fatal("selected vector forms have the wrong target")
	}
	f := memoryFixture()
	f.Insts[0].Op = OpAMD64V128Load
	if err := Verify(f); err == nil || !strings.Contains(err.Error(), "invalid arm64 opcode") {
		t.Fatalf("Verify cross-target selected opcode = %v", err)
	}
	f.Insts[0].Op = OpAMD64I32Add
	if err := Verify(f); err == nil || !strings.Contains(err.Error(), "invalid arm64 opcode") {
		t.Fatalf("Verify cross-target selected scalar opcode = %v", err)
	}
}

func TestSelectTargetOpcodesV128Foundation(t *testing.T) {
	for _, test := range []struct {
		name   string
		target Target
		want   []MOpcode
	}{
		{"amd64", TargetAMD64, []MOpcode{OpAMD64V128Const, OpAMD64V128Load, OpAMD64V128And, OpAMD64V128Store}},
		{"arm64", TargetARM64, []MOpcode{OpARM64V128Const, OpARM64V128Load, OpARM64V128And, OpARM64V128Store}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := vectorFoundationFixture(test.target)
			count, err := SelectTargetOpcodes(f)
			if err != nil {
				t.Fatal(err)
			}
			if count != len(test.want) {
				t.Fatalf("selected %d operations, want %d", count, len(test.want))
			}
			for index, want := range test.want {
				if got := f.Insts[index].Op; got != want {
					t.Fatalf("instruction %d opcode = %d, want %d", index, got, want)
				}
			}
		})
	}
}

func TestSelectedOpcodeTargetRanges(t *testing.T) {
	for _, test := range []struct {
		name   string
		op     MOpcode
		target Target
		want   bool
	}{
		{"amd64 first", OpAMD64V128Move, TargetAMD64, true},
		{"amd64 last", opAMD64SelectedEnd - 1, TargetAMD64, true},
		{"amd64 end", opAMD64SelectedEnd, TargetAMD64, false},
		{"amd64 on arm64", OpAMD64I32Add, TargetARM64, false},
		{"arm64 first", OpARM64V128Move, TargetARM64, true},
		{"arm64 last", opARM64SelectedEnd - 1, TargetARM64, true},
		{"arm64 end", opARM64SelectedEnd, TargetARM64, false},
		{"arm64 on amd64", OpARM64I32Add, TargetAMD64, false},
		{"generic", wasm.InstrI32Add, TargetAMD64, false},
		{"invalid target", OpAMD64I32Add, TargetInvalid, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := IsSelectedOpcodeForTarget(test.op, test.target); got != test.want {
				t.Fatalf("IsSelectedOpcodeForTarget(%d, %s) = %t, want %t", test.op, test.target, got, test.want)
			}
		})
	}
}

func TestSelectTargetOpcodesIntegerAddSub(t *testing.T) {
	for _, test := range []struct {
		name   string
		target Target
		want   [4]MOpcode
	}{
		{"amd64", TargetAMD64, [4]MOpcode{OpAMD64I32Add, OpAMD64I64Add, OpAMD64I32Sub, OpAMD64I64Sub}},
		{"arm64", TargetARM64, [4]MOpcode{OpARM64I32Add, OpARM64I64Add, OpARM64I32Sub, OpARM64I64Sub}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, op := range []byte{0x6a, 0x7c, 0x6b, 0x7d} {
				type_ := wasm.I32
				if index&1 != 0 {
					type_ = wasm.I64
				}
				m := machineModule([]wasm.ValType{type_, type_}, []wasm.ValType{type_}, []byte{0x20, 0, 0x20, 1, op, 0x0b})
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != [4]MOpcode{wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub}[index] {
					t.Fatalf("instruction %d semantic opcode = %d", index, got)
				}
			}
		})
	}
}

func TestSelectARM64ImmediateOpcodesInteger(t *testing.T) {
	for _, test := range []struct {
		name     string
		type_    wasm.ValType
		op       byte
		semantic MOpcode
		want     MOpcode
	}{
		{"i32.add", wasm.I32, 0x6a, wasm.InstrI32Add, OpARM64I32AddImmediate},
		{"i64.add", wasm.I64, 0x7c, wasm.InstrI64Add, OpARM64I64AddImmediate},
		{"i32.sub", wasm.I32, 0x6b, wasm.InstrI32Sub, OpARM64I32SubImmediate},
		{"i64.sub", wasm.I64, 0x7d, wasm.InstrI64Sub, OpARM64I64SubImmediate},
		{"i32.and", wasm.I32, 0x71, wasm.InstrI32And, OpARM64I32AndImmediate},
		{"i64.and", wasm.I64, 0x83, wasm.InstrI64And, OpARM64I64AndImmediate},
		{"i32.or", wasm.I32, 0x72, wasm.InstrI32Or, OpARM64I32OrImmediate},
		{"i64.or", wasm.I64, 0x84, wasm.InstrI64Or, OpARM64I64OrImmediate},
		{"i32.xor", wasm.I32, 0x73, wasm.InstrI32Xor, OpARM64I32XorImmediate},
		{"i64.xor", wasm.I64, 0x85, wasm.InstrI64Xor, OpARM64I64XorImmediate},
		{"i32.shl", wasm.I32, 0x74, wasm.InstrI32Shl, OpARM64I32ShlImmediate},
		{"i64.shl", wasm.I64, 0x86, wasm.InstrI64Shl, OpARM64I64ShlImmediate},
		{"i32.shr_s", wasm.I32, 0x75, wasm.InstrI32ShrS, OpARM64I32ShrSImmediate},
		{"i64.shr_s", wasm.I64, 0x87, wasm.InstrI64ShrS, OpARM64I64ShrSImmediate},
		{"i32.shr_u", wasm.I32, 0x76, wasm.InstrI32ShrU, OpARM64I32ShrUImmediate},
		{"i64.shr_u", wasm.I64, 0x88, wasm.InstrI64ShrU, OpARM64I64ShrUImmediate},
		{"i32.rotl", wasm.I32, 0x77, wasm.InstrI32Rotl, OpARM64I32RotlImmediate},
		{"i64.rotl", wasm.I64, 0x89, wasm.InstrI64Rotl, OpARM64I64RotlImmediate},
		{"i32.rotr", wasm.I32, 0x78, wasm.InstrI32Rotr, OpARM64I32RotrImmediate},
		{"i64.rotr", wasm.I64, 0x8a, wasm.InstrI64Rotr, OpARM64I64RotrImmediate},
	} {
		t.Run(test.name, func(t *testing.T) {
			constant := byte(0x41)
			if test.type_ == wasm.I64 {
				constant = 0x42
			}
			m := machineModule([]wasm.ValType{test.type_}, []wasm.ValType{test.type_}, []byte{0x20, 0, constant, 7, test.op, 0x0b})
			f := buildMachineTest(t, TargetARM64, m)
			if len(f.Insts) != 2 {
				t.Fatalf("instructions = %#v, want const and operation", f.Insts)
			}
			if _, err := SelectTargetOpcodes(f); err != nil {
				t.Fatal(err)
			}
			selected, err := SelectARM64ImmediateOpcodes(f, []uint16{0, 1}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if selected != 1 || f.Insts[1].Op != test.want || f.Insts[1].Aux != 7 {
				t.Fatalf("selected instructions = %#v, count %d, want %d with immediate 7", f.Insts, selected, test.want)
			}
			if got := SemanticOpcode(f.Insts[1].Op); got != test.semantic {
				t.Fatalf("semantic opcode = %d, want %d", got, test.semantic)
			}
		})
	}
}

func TestSelectARM64ImmediateOpcodesComparison(t *testing.T) {
	for _, test := range []struct {
		name     string
		type_    wasm.ValType
		op       byte
		semantic MOpcode
		want     MOpcode
	}{
		{"i32.eq", wasm.I32, 0x46, wasm.InstrI32Eq, OpARM64I32EqImmediate},
		{"i64.eq", wasm.I64, 0x51, wasm.InstrI64Eq, OpARM64I64EqImmediate},
		{"i32.ne", wasm.I32, 0x47, wasm.InstrI32Ne, OpARM64I32NeImmediate},
		{"i64.ne", wasm.I64, 0x52, wasm.InstrI64Ne, OpARM64I64NeImmediate},
		{"i32.lt_s", wasm.I32, 0x48, wasm.InstrI32LtS, OpARM64I32LtSImmediate},
		{"i64.lt_s", wasm.I64, 0x53, wasm.InstrI64LtS, OpARM64I64LtSImmediate},
		{"i32.lt_u", wasm.I32, 0x49, wasm.InstrI32LtU, OpARM64I32LtUImmediate},
		{"i64.lt_u", wasm.I64, 0x54, wasm.InstrI64LtU, OpARM64I64LtUImmediate},
		{"i32.gt_s", wasm.I32, 0x4a, wasm.InstrI32GtS, OpARM64I32GtSImmediate},
		{"i64.gt_s", wasm.I64, 0x55, wasm.InstrI64GtS, OpARM64I64GtSImmediate},
		{"i32.gt_u", wasm.I32, 0x4b, wasm.InstrI32GtU, OpARM64I32GtUImmediate},
		{"i64.gt_u", wasm.I64, 0x56, wasm.InstrI64GtU, OpARM64I64GtUImmediate},
		{"i32.le_s", wasm.I32, 0x4c, wasm.InstrI32LeS, OpARM64I32LeSImmediate},
		{"i64.le_s", wasm.I64, 0x57, wasm.InstrI64LeS, OpARM64I64LeSImmediate},
		{"i32.le_u", wasm.I32, 0x4d, wasm.InstrI32LeU, OpARM64I32LeUImmediate},
		{"i64.le_u", wasm.I64, 0x58, wasm.InstrI64LeU, OpARM64I64LeUImmediate},
		{"i32.ge_s", wasm.I32, 0x4e, wasm.InstrI32GeS, OpARM64I32GeSImmediate},
		{"i64.ge_s", wasm.I64, 0x59, wasm.InstrI64GeS, OpARM64I64GeSImmediate},
		{"i32.ge_u", wasm.I32, 0x4f, wasm.InstrI32GeU, OpARM64I32GeUImmediate},
		{"i64.ge_u", wasm.I64, 0x5a, wasm.InstrI64GeU, OpARM64I64GeUImmediate},
	} {
		t.Run(test.name, func(t *testing.T) {
			constant := byte(0x41)
			if test.type_ == wasm.I64 {
				constant = 0x42
			}
			m := machineModule([]wasm.ValType{test.type_}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, constant, 7, test.op, 0x0b})
			f := buildMachineTest(t, TargetARM64, m)
			if len(f.Insts) != 2 {
				t.Fatalf("instructions = %#v, want const and comparison", f.Insts)
			}
			if _, err := SelectTargetOpcodes(f); err != nil {
				t.Fatal(err)
			}
			selected, err := SelectARM64ImmediateOpcodes(f, []uint16{0, 1}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if selected != 1 || f.Insts[1].Op != test.want || f.Insts[1].Aux != 7 {
				t.Fatalf("selected instructions = %#v, count %d, want %d with immediate 7", f.Insts, selected, test.want)
			}
			if got := SemanticOpcode(f.Insts[1].Op); got != test.semantic {
				t.Fatalf("semantic opcode = %d, want %d", got, test.semantic)
			}
		})
	}
}

func TestSelectARM64ImmediateOpcodesSIMDShift(t *testing.T) {
	for _, test := range []struct {
		name string
		op   MOpcode
		want MOpcode
	}{
		{"i8x16.shl", wasm.InstrI8x16Shl, OpARM64I8x16ShlImmediate},
		{"i8x16.shr_s", wasm.InstrI8x16ShrS, OpARM64I8x16ShrSImmediate},
		{"i8x16.shr_u", wasm.InstrI8x16ShrU, OpARM64I8x16ShrUImmediate},
		{"i16x8.shl", wasm.InstrI16x8Shl, OpARM64I16x8ShlImmediate},
		{"i16x8.shr_s", wasm.InstrI16x8ShrS, OpARM64I16x8ShrSImmediate},
		{"i16x8.shr_u", wasm.InstrI16x8ShrU, OpARM64I16x8ShrUImmediate},
		{"i32x4.shl", wasm.InstrI32x4Shl, OpARM64I32x4ShlImmediate},
		{"i32x4.shr_s", wasm.InstrI32x4ShrS, OpARM64I32x4ShrSImmediate},
		{"i32x4.shr_u", wasm.InstrI32x4ShrU, OpARM64I32x4ShrUImmediate},
		{"i64x2.shl", wasm.InstrI64x2Shl, OpARM64I64x2ShlImmediate},
		{"i64x2.shr_s", wasm.InstrI64x2ShrS, OpARM64I64x2ShrSImmediate},
		{"i64x2.shr_u", wasm.InstrI64x2ShrU, OpARM64I64x2ShrUImmediate},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := &Func{
				Target: TargetARM64,
				VRegs: []VRegData{
					{},
					{Type: TypeI32, Bank: BankGPR},
					{Type: TypeV128, Bank: BankFPR},
					{Type: TypeV128, Bank: BankFPR},
				},
				Operands: []Operand{
					{Reg: 2, Fixed: NoFixedReg, Bank: BankFPR, Flags: OperandUse},
					{Reg: 1, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse},
				},
				Insts: []Inst{
					{Op: wasm.InstrI32Const, Aux: 13, Result: 1},
					{Op: test.op, OperandStart: 0, OperandCount: 2, Result: 3},
				},
				Blocks: []Block{{InstCount: 2}},
				SIMD:   []railssa.SemanticSIMDImmediate{{Instruction: 1}},
			}
			if _, err := SelectTargetOpcodes(f); err != nil {
				t.Fatal(err)
			}
			producers := []uint16{0, 1}
			selected, err := SelectARM64ImmediateOpcodes(f, producers, nil)
			if err != nil {
				t.Fatal(err)
			}
			if selected != 1 || f.Insts[1].Op != test.want || f.Insts[1].Aux != 13 || producers[1] != 0 {
				t.Fatalf("selected instructions = %#v, producers %v, count %d, want %d with immediate 13", f.Insts, producers, selected, test.want)
			}
		})
	}
}

func TestSelectARM64ImmediateOpcodesWideRelation(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{0x20, 0, 0x42, 7, 0x7c, 0x0b})
	f := buildMachineTest(t, TargetARM64, m)
	if _, err := SelectTargetOpcodes(f); err != nil {
		t.Fatal(err)
	}
	producers := []uint32{0, 1}
	selected, err := SelectARM64ImmediateOpcodes(f, nil, producers)
	if err != nil {
		t.Fatal(err)
	}
	if selected != 1 || f.Insts[1].Op != OpARM64I64AddImmediate || f.Insts[1].Aux != 7 || producers[1] != 0 {
		t.Fatalf("selected instructions = %#v, producers %v, count %d", f.Insts, producers, selected)
	}
}

func TestSelectTargetOpcodesIntegerLogical(t *testing.T) {
	for _, test := range []struct {
		name   string
		target Target
		want   [6]MOpcode
	}{
		{"amd64", TargetAMD64, [6]MOpcode{OpAMD64I32And, OpAMD64I64And, OpAMD64I32Or, OpAMD64I64Or, OpAMD64I32Xor, OpAMD64I64Xor}},
		{"arm64", TargetARM64, [6]MOpcode{OpARM64I32And, OpARM64I64And, OpARM64I32Or, OpARM64I64Or, OpARM64I32Xor, OpARM64I64Xor}},
	} {
		t.Run(test.name, func(t *testing.T) {
			operations := [6]MOpcode{wasm.InstrI32And, wasm.InstrI64And, wasm.InstrI32Or, wasm.InstrI64Or, wasm.InstrI32Xor, wasm.InstrI64Xor}
			for index, op := range []byte{0x71, 0x83, 0x72, 0x84, 0x73, 0x85} {
				type_ := wasm.I32
				if index&1 != 0 {
					type_ = wasm.I64
				}
				m := machineModule([]wasm.ValType{type_, type_}, []wasm.ValType{type_}, []byte{0x20, 0, 0x20, 1, op, 0x0b})
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operations[index] {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operations[index])
				}
			}
		})
	}
}

func TestSelectTargetOpcodesIntegerMultiply(t *testing.T) {
	for _, test := range []struct {
		name   string
		target Target
		want   [2]MOpcode
	}{
		{"amd64", TargetAMD64, [2]MOpcode{OpAMD64I32Mul, OpAMD64I64Mul}},
		{"arm64", TargetARM64, [2]MOpcode{OpARM64I32Mul, OpARM64I64Mul}},
	} {
		t.Run(test.name, func(t *testing.T) {
			operations := [2]MOpcode{wasm.InstrI32Mul, wasm.InstrI64Mul}
			for index, op := range []byte{0x6c, 0x7e} {
				type_ := wasm.I32
				if index != 0 {
					type_ = wasm.I64
				}
				m := machineModule([]wasm.ValType{type_, type_}, []wasm.ValType{type_}, []byte{0x20, 0, 0x20, 1, op, 0x0b})
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operations[index] {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operations[index])
				}
			}
		})
	}
}

func TestSelectTargetOpcodesIntegerShifts(t *testing.T) {
	operations := [10]MOpcode{
		wasm.InstrI32Shl, wasm.InstrI64Shl, wasm.InstrI32ShrS, wasm.InstrI64ShrS, wasm.InstrI32ShrU,
		wasm.InstrI64ShrU, wasm.InstrI32Rotl, wasm.InstrI64Rotl, wasm.InstrI32Rotr, wasm.InstrI64Rotr,
	}
	for _, test := range []struct {
		name   string
		target Target
		want   [10]MOpcode
	}{
		{"amd64", TargetAMD64, [10]MOpcode{
			OpAMD64I32Shl, OpAMD64I64Shl, OpAMD64I32ShrS, OpAMD64I64ShrS, OpAMD64I32ShrU,
			OpAMD64I64ShrU, OpAMD64I32Rotl, OpAMD64I64Rotl, OpAMD64I32Rotr, OpAMD64I64Rotr,
		}},
		{"arm64", TargetARM64, [10]MOpcode{
			OpARM64I32Shl, OpARM64I64Shl, OpARM64I32ShrS, OpARM64I64ShrS, OpARM64I32ShrU,
			OpARM64I64ShrU, OpARM64I32Rotl, OpARM64I64Rotl, OpARM64I32Rotr, OpARM64I64Rotr,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, op := range []byte{0x74, 0x86, 0x75, 0x87, 0x76, 0x88, 0x77, 0x89, 0x78, 0x8a} {
				type_ := wasm.I32
				if index&1 != 0 {
					type_ = wasm.I64
				}
				m := machineModule([]wasm.ValType{type_, type_}, []wasm.ValType{type_}, []byte{0x20, 0, 0x20, 1, op, 0x0b})
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operations[index] {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operations[index])
				}
			}
		})
	}
}

func TestSelectTargetOpcodesIntegerUnary(t *testing.T) {
	operations := [6]MOpcode{
		wasm.InstrI32Clz, wasm.InstrI64Clz, wasm.InstrI32Ctz,
		wasm.InstrI64Ctz, wasm.InstrI32Popcnt, wasm.InstrI64Popcnt,
	}
	for _, test := range []struct {
		name   string
		target Target
		want   [6]MOpcode
	}{
		{"amd64", TargetAMD64, [6]MOpcode{
			OpAMD64I32Clz, OpAMD64I64Clz, OpAMD64I32Ctz,
			OpAMD64I64Ctz, OpAMD64I32Popcnt, OpAMD64I64Popcnt,
		}},
		{"arm64", TargetARM64, [6]MOpcode{
			OpARM64I32Clz, OpARM64I64Clz, OpARM64I32Ctz,
			OpARM64I64Ctz, OpARM64I32Popcnt, OpARM64I64Popcnt,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, encoding := range []byte{0x67, 0x79, 0x68, 0x7a, 0x69, 0x7b} {
				type_ := wasm.I32
				if index&1 != 0 {
					type_ = wasm.I64
				}
				m := machineModule([]wasm.ValType{type_}, []wasm.ValType{type_}, []byte{0x20, 0, encoding, 0x0b})
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operations[index] {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operations[index])
				}
			}
		})
	}
}

func TestSelectTargetOpcodesScalarFloatArithmetic(t *testing.T) {
	operations := [8]MOpcode{
		wasm.InstrF32Add, wasm.InstrF64Add, wasm.InstrF32Sub, wasm.InstrF64Sub,
		wasm.InstrF32Mul, wasm.InstrF64Mul, wasm.InstrF32Div, wasm.InstrF64Div,
	}
	for _, test := range []struct {
		name   string
		target Target
		want   [8]MOpcode
	}{
		{"amd64", TargetAMD64, [8]MOpcode{
			OpAMD64F32AddScalar, OpAMD64F64AddScalar, OpAMD64F32SubScalar, OpAMD64F64SubScalar,
			OpAMD64F32MulScalar, OpAMD64F64MulScalar, OpAMD64F32DivScalar, OpAMD64F64DivScalar,
		}},
		{"arm64", TargetARM64, [8]MOpcode{
			OpARM64F32AddScalar, OpARM64F64AddScalar, OpARM64F32SubScalar, OpARM64F64SubScalar,
			OpARM64F32MulScalar, OpARM64F64MulScalar, OpARM64F32DivScalar, OpARM64F64DivScalar,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, encoding := range []byte{0x92, 0xa0, 0x93, 0xa1, 0x94, 0xa2, 0x95, 0xa3} {
				type_ := wasm.F32
				if index&1 != 0 {
					type_ = wasm.F64
				}
				m := machineModule([]wasm.ValType{type_, type_}, []wasm.ValType{type_}, []byte{0x20, 0, 0x20, 1, encoding, 0x0b})
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operations[index] {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operations[index])
				}
			}
		})
	}
}

func TestSelectTargetOpcodesIntegerConversions(t *testing.T) {
	operations := [8]MOpcode{
		wasm.InstrI32WrapI64, wasm.InstrI64ExtendI32S, wasm.InstrI64ExtendI32U,
		wasm.InstrI32Extend8S, wasm.InstrI32Extend16S,
		wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S,
	}
	for _, test := range []struct {
		name   string
		target Target
		want   [8]MOpcode
	}{
		{"amd64", TargetAMD64, [8]MOpcode{
			OpAMD64I32WrapI64, OpAMD64I64ExtendI32S, OpAMD64I64ExtendI32U,
			OpAMD64I32Extend8S, OpAMD64I32Extend16S,
			OpAMD64I64Extend8S, OpAMD64I64Extend16S, OpAMD64I64Extend32S,
		}},
		{"arm64", TargetARM64, [8]MOpcode{
			OpARM64I32WrapI64, OpARM64I64ExtendI32S, OpARM64I64ExtendI32U,
			OpARM64I32Extend8S, OpARM64I32Extend16S,
			OpARM64I64Extend8S, OpARM64I64Extend16S, OpARM64I64Extend32S,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, encoding := range []byte{0xa7, 0xac, 0xad, 0xc0, 0xc1, 0xc2, 0xc3, 0xc4} {
				parameterType, resultType := wasm.I32, wasm.I64
				if index == 0 {
					parameterType, resultType = wasm.I64, wasm.I32
				} else if index == 3 || index == 4 {
					resultType = wasm.I32
				} else if index >= 5 {
					parameterType = wasm.I64
				}
				m := machineModule([]wasm.ValType{parameterType}, []wasm.ValType{resultType}, []byte{0x20, 0, encoding, 0x0b})
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operations[index] {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operations[index])
				}
			}
		})
	}
}

func TestSelectTargetOpcodesScalarFloatComparisons(t *testing.T) {
	operations := [12]MOpcode{
		wasm.InstrF32Eq, wasm.InstrF64Eq, wasm.InstrF32Ne, wasm.InstrF64Ne,
		wasm.InstrF32Lt, wasm.InstrF64Lt, wasm.InstrF32Gt, wasm.InstrF64Gt,
		wasm.InstrF32Le, wasm.InstrF64Le, wasm.InstrF32Ge, wasm.InstrF64Ge,
	}
	for _, test := range []struct {
		name   string
		target Target
		want   [12]MOpcode
	}{
		{"amd64", TargetAMD64, [12]MOpcode{
			OpAMD64F32EqScalar, OpAMD64F64EqScalar, OpAMD64F32NeScalar, OpAMD64F64NeScalar,
			OpAMD64F32LtScalar, OpAMD64F64LtScalar, OpAMD64F32GtScalar, OpAMD64F64GtScalar,
			OpAMD64F32LeScalar, OpAMD64F64LeScalar, OpAMD64F32GeScalar, OpAMD64F64GeScalar,
		}},
		{"arm64", TargetARM64, [12]MOpcode{
			OpARM64F32EqScalar, OpARM64F64EqScalar, OpARM64F32NeScalar, OpARM64F64NeScalar,
			OpARM64F32LtScalar, OpARM64F64LtScalar, OpARM64F32GtScalar, OpARM64F64GtScalar,
			OpARM64F32LeScalar, OpARM64F64LeScalar, OpARM64F32GeScalar, OpARM64F64GeScalar,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, encoding := range []byte{0x5b, 0x61, 0x5c, 0x62, 0x5d, 0x63, 0x5e, 0x64, 0x5f, 0x65, 0x60, 0x66} {
				type_ := wasm.F32
				if index&1 != 0 {
					type_ = wasm.F64
				}
				m := machineModule([]wasm.ValType{type_, type_}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, 0x20, 1, encoding, 0x0b})
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operations[index] {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operations[index])
				}
			}
		})
	}
}

func TestSelectTargetOpcodesIntegerDivision(t *testing.T) {
	operations := [8]MOpcode{
		wasm.InstrI32DivS, wasm.InstrI32DivU, wasm.InstrI32RemS, wasm.InstrI32RemU,
		wasm.InstrI64DivS, wasm.InstrI64DivU, wasm.InstrI64RemS, wasm.InstrI64RemU,
	}
	for _, test := range []struct {
		name   string
		target Target
		want   [8]MOpcode
	}{
		{"amd64", TargetAMD64, [8]MOpcode{
			OpAMD64I32DivS, OpAMD64I32DivU, OpAMD64I32RemS, OpAMD64I32RemU,
			OpAMD64I64DivS, OpAMD64I64DivU, OpAMD64I64RemS, OpAMD64I64RemU,
		}},
		{"arm64", TargetARM64, [8]MOpcode{
			OpARM64I32DivS, OpARM64I32DivU, OpARM64I32RemS, OpARM64I32RemU,
			OpARM64I64DivS, OpARM64I64DivU, OpARM64I64RemS, OpARM64I64RemU,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, encoding := range []byte{0x6d, 0x6e, 0x6f, 0x70, 0x7f, 0x80, 0x81, 0x82} {
				type_ := wasm.I32
				if index >= 4 {
					type_ = wasm.I64
				}
				m := machineModule([]wasm.ValType{type_, type_}, []wasm.ValType{type_}, []byte{0x20, 0, 0x20, 1, encoding, 0x0b})
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operations[index] {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operations[index])
				}
			}
		})
	}
}

func TestSelectTargetOpcodesFloatConversions(t *testing.T) {
	operations := [14]MOpcode{
		wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U, wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U,
		wasm.InstrF32DemoteF64, wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U, wasm.InstrF64ConvertI64S,
		wasm.InstrF64ConvertI64U, wasm.InstrF64PromoteF32, wasm.InstrI32ReinterpretF32, wasm.InstrI64ReinterpretF64,
		wasm.InstrF32ReinterpretI32, wasm.InstrF64ReinterpretI64,
	}
	inputs := [14]wasm.ValType{
		wasm.I32, wasm.I32, wasm.I64, wasm.I64, wasm.F64, wasm.I32, wasm.I32,
		wasm.I64, wasm.I64, wasm.F32, wasm.F32, wasm.F64, wasm.I32, wasm.I64,
	}
	results := [14]wasm.ValType{
		wasm.F32, wasm.F32, wasm.F32, wasm.F32, wasm.F32, wasm.F64, wasm.F64,
		wasm.F64, wasm.F64, wasm.F64, wasm.I32, wasm.I64, wasm.F32, wasm.F64,
	}
	for _, test := range []struct {
		name   string
		target Target
		want   [14]MOpcode
	}{
		{"amd64", TargetAMD64, [14]MOpcode{
			OpAMD64F32ConvertI32S, OpAMD64F32ConvertI32U, OpAMD64F32ConvertI64S, OpAMD64F32ConvertI64U,
			OpAMD64F32DemoteF64, OpAMD64F64ConvertI32S, OpAMD64F64ConvertI32U, OpAMD64F64ConvertI64S,
			OpAMD64F64ConvertI64U, OpAMD64F64PromoteF32, OpAMD64I32ReinterpretF32, OpAMD64I64ReinterpretF64,
			OpAMD64F32ReinterpretI32, OpAMD64F64ReinterpretI64,
		}},
		{"arm64", TargetARM64, [14]MOpcode{
			OpARM64F32ConvertI32S, OpARM64F32ConvertI32U, OpARM64F32ConvertI64S, OpARM64F32ConvertI64U,
			OpARM64F32DemoteF64, OpARM64F64ConvertI32S, OpARM64F64ConvertI32U, OpARM64F64ConvertI64S,
			OpARM64F64ConvertI64U, OpARM64F64PromoteF32, OpARM64I32ReinterpretF32, OpARM64I64ReinterpretF64,
			OpARM64F32ReinterpretI32, OpARM64F64ReinterpretI64,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, encoding := range []byte{0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xbb, 0xbc, 0xbd, 0xbe, 0xbf} {
				m := machineModule([]wasm.ValType{inputs[index]}, []wasm.ValType{results[index]}, []byte{0x20, 0, encoding, 0x0b})
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operations[index] {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operations[index])
				}
			}
		})
	}
}

func TestSelectTargetOpcodesFloatTruncations(t *testing.T) {
	operations := [16]MOpcode{
		wasm.InstrI32TruncF32S, wasm.InstrI32TruncF32U, wasm.InstrI32TruncF64S, wasm.InstrI32TruncF64U,
		wasm.InstrI64TruncF32S, wasm.InstrI64TruncF32U, wasm.InstrI64TruncF64S, wasm.InstrI64TruncF64U,
		wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U, wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U,
		wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U, wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U,
	}
	inputs := [16]wasm.ValType{
		wasm.F32, wasm.F32, wasm.F64, wasm.F64, wasm.F32, wasm.F32, wasm.F64, wasm.F64,
		wasm.F32, wasm.F32, wasm.F64, wasm.F64, wasm.F32, wasm.F32, wasm.F64, wasm.F64,
	}
	results := [16]wasm.ValType{
		wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I64, wasm.I64, wasm.I64, wasm.I64,
		wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I64, wasm.I64, wasm.I64, wasm.I64,
	}
	encodings := [16][]byte{
		{0xa8}, {0xa9}, {0xaa}, {0xab}, {0xae}, {0xaf}, {0xb0}, {0xb1},
		{0xfc, 0}, {0xfc, 1}, {0xfc, 2}, {0xfc, 3}, {0xfc, 4}, {0xfc, 5}, {0xfc, 6}, {0xfc, 7},
	}
	for _, test := range []struct {
		name   string
		target Target
		want   [16]MOpcode
	}{
		{"amd64", TargetAMD64, [16]MOpcode{
			OpAMD64I32TruncF32S, OpAMD64I32TruncF32U, OpAMD64I32TruncF64S, OpAMD64I32TruncF64U,
			OpAMD64I64TruncF32S, OpAMD64I64TruncF32U, OpAMD64I64TruncF64S, OpAMD64I64TruncF64U,
			OpAMD64I32TruncSatF32S, OpAMD64I32TruncSatF32U, OpAMD64I32TruncSatF64S, OpAMD64I32TruncSatF64U,
			OpAMD64I64TruncSatF32S, OpAMD64I64TruncSatF32U, OpAMD64I64TruncSatF64S, OpAMD64I64TruncSatF64U,
		}},
		{"arm64", TargetARM64, [16]MOpcode{
			OpARM64I32TruncF32S, OpARM64I32TruncF32U, OpARM64I32TruncF64S, OpARM64I32TruncF64U,
			OpARM64I64TruncF32S, OpARM64I64TruncF32U, OpARM64I64TruncF64S, OpARM64I64TruncF64U,
			OpARM64I32TruncSatF32S, OpARM64I32TruncSatF32U, OpARM64I32TruncSatF64S, OpARM64I32TruncSatF64U,
			OpARM64I64TruncSatF32S, OpARM64I64TruncSatF32U, OpARM64I64TruncSatF64S, OpARM64I64TruncSatF64U,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, encoding := range encodings {
				body := append([]byte{0x20, 0}, encoding...)
				body = append(body, 0x0b)
				m := machineModule([]wasm.ValType{inputs[index]}, []wasm.ValType{results[index]}, body)
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operations[index] {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operations[index])
				}
			}
		})
	}
}

func TestSelectTargetOpcodesScalarFloatUnary(t *testing.T) {
	operations := [14]MOpcode{
		wasm.InstrF32Abs, wasm.InstrF32Neg, wasm.InstrF32Ceil, wasm.InstrF32Floor, wasm.InstrF32Trunc, wasm.InstrF32Nearest, wasm.InstrF32Sqrt,
		wasm.InstrF64Abs, wasm.InstrF64Neg, wasm.InstrF64Ceil, wasm.InstrF64Floor, wasm.InstrF64Trunc, wasm.InstrF64Nearest, wasm.InstrF64Sqrt,
	}
	for _, test := range []struct {
		name   string
		target Target
		want   [14]MOpcode
	}{
		{"amd64", TargetAMD64, [14]MOpcode{
			OpAMD64F32AbsScalar, OpAMD64F32NegScalar, OpAMD64F32CeilScalar, OpAMD64F32FloorScalar, OpAMD64F32TruncScalar, OpAMD64F32NearestScalar, OpAMD64F32SqrtScalar,
			OpAMD64F64AbsScalar, OpAMD64F64NegScalar, OpAMD64F64CeilScalar, OpAMD64F64FloorScalar, OpAMD64F64TruncScalar, OpAMD64F64NearestScalar, OpAMD64F64SqrtScalar,
		}},
		{"arm64", TargetARM64, [14]MOpcode{
			OpARM64F32AbsScalar, OpARM64F32NegScalar, OpARM64F32CeilScalar, OpARM64F32FloorScalar, OpARM64F32TruncScalar, OpARM64F32NearestScalar, OpARM64F32SqrtScalar,
			OpARM64F64AbsScalar, OpARM64F64NegScalar, OpARM64F64CeilScalar, OpARM64F64FloorScalar, OpARM64F64TruncScalar, OpARM64F64NearestScalar, OpARM64F64SqrtScalar,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, encoding := range []byte{0x8b, 0x8c, 0x8d, 0x8e, 0x8f, 0x90, 0x91, 0x99, 0x9a, 0x9b, 0x9c, 0x9d, 0x9e, 0x9f} {
				type_ := wasm.F32
				if index >= 7 {
					type_ = wasm.F64
				}
				m := machineModule([]wasm.ValType{type_}, []wasm.ValType{type_}, []byte{0x20, 0, encoding, 0x0b})
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operations[index] {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operations[index])
				}
			}
		})
	}
}

func TestSelectTargetOpcodesScalarFloatMinMaxCopysign(t *testing.T) {
	operations := [6]MOpcode{
		wasm.InstrF32Min, wasm.InstrF32Max, wasm.InstrF32Copysign,
		wasm.InstrF64Min, wasm.InstrF64Max, wasm.InstrF64Copysign,
	}
	for _, test := range []struct {
		name   string
		target Target
		want   [6]MOpcode
	}{
		{"amd64", TargetAMD64, [6]MOpcode{
			OpAMD64F32MinScalar, OpAMD64F32MaxScalar, OpAMD64F32CopysignScalar,
			OpAMD64F64MinScalar, OpAMD64F64MaxScalar, OpAMD64F64CopysignScalar,
		}},
		{"arm64", TargetARM64, [6]MOpcode{
			OpARM64F32MinScalar, OpARM64F32MaxScalar, OpARM64F32CopysignScalar,
			OpARM64F64MinScalar, OpARM64F64MaxScalar, OpARM64F64CopysignScalar,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, encoding := range []byte{0x96, 0x97, 0x98, 0xa4, 0xa5, 0xa6} {
				type_ := wasm.F32
				if index >= 3 {
					type_ = wasm.F64
				}
				m := machineModule([]wasm.ValType{type_, type_}, []wasm.ValType{type_}, []byte{0x20, 0, 0x20, 1, encoding, 0x0b})
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operations[index] {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operations[index])
				}
			}
		})
	}
}

func TestSelectTargetOpcodesScalarMemory(t *testing.T) {
	operations := [23]MOpcode{
		wasm.InstrI32Load, wasm.InstrI64Load, wasm.InstrF32Load, wasm.InstrF64Load,
		wasm.InstrI32Load8S, wasm.InstrI32Load8U, wasm.InstrI32Load16S, wasm.InstrI32Load16U,
		wasm.InstrI64Load8S, wasm.InstrI64Load8U, wasm.InstrI64Load16S, wasm.InstrI64Load16U,
		wasm.InstrI64Load32S, wasm.InstrI64Load32U,
		wasm.InstrI32Store, wasm.InstrI64Store, wasm.InstrF32Store, wasm.InstrF64Store,
		wasm.InstrI32Store8, wasm.InstrI32Store16, wasm.InstrI64Store8, wasm.InstrI64Store16, wasm.InstrI64Store32,
	}
	loadTypes := [14]wasm.ValType{
		wasm.I32, wasm.I64, wasm.F32, wasm.F64,
		wasm.I32, wasm.I32, wasm.I32, wasm.I32,
		wasm.I64, wasm.I64, wasm.I64, wasm.I64, wasm.I64, wasm.I64,
	}
	storeTypes := [9]wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.I32, wasm.I32, wasm.I64, wasm.I64, wasm.I64}
	for _, test := range []struct {
		name   string
		target Target
		want   [23]MOpcode
	}{
		{"amd64", TargetAMD64, [23]MOpcode{
			OpAMD64I32Load, OpAMD64I64Load, OpAMD64F32Load, OpAMD64F64Load,
			OpAMD64I32Load8S, OpAMD64I32Load8U, OpAMD64I32Load16S, OpAMD64I32Load16U,
			OpAMD64I64Load8S, OpAMD64I64Load8U, OpAMD64I64Load16S, OpAMD64I64Load16U,
			OpAMD64I64Load32S, OpAMD64I64Load32U,
			OpAMD64I32Store, OpAMD64I64Store, OpAMD64F32Store, OpAMD64F64Store,
			OpAMD64I32Store8, OpAMD64I32Store16, OpAMD64I64Store8, OpAMD64I64Store16, OpAMD64I64Store32,
		}},
		{"arm64", TargetARM64, [23]MOpcode{
			OpARM64I32Load, OpARM64I64Load, OpARM64F32Load, OpARM64F64Load,
			OpARM64I32Load8S, OpARM64I32Load8U, OpARM64I32Load16S, OpARM64I32Load16U,
			OpARM64I64Load8S, OpARM64I64Load8U, OpARM64I64Load16S, OpARM64I64Load16U,
			OpARM64I64Load32S, OpARM64I64Load32U,
			OpARM64I32Store, OpARM64I64Store, OpARM64F32Store, OpARM64F64Store,
			OpARM64I32Store8, OpARM64I32Store16, OpARM64I64Store8, OpARM64I64Store16, OpARM64I64Store32,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, operation := range operations {
				params := []wasm.ValType{wasm.I32}
				var results []wasm.ValType
				body := []byte{0x20, 0}
				if index < len(loadTypes) {
					results = []wasm.ValType{loadTypes[index]}
				} else {
					params = append(params, storeTypes[index-len(loadTypes)])
					body = append(body, 0x20, 1)
				}
				body = append(body, byte(0x28+index), 0, 0, 0x0b)
				source := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
					wasmtest.Section(3, wasmtest.Vec([]byte{0})),
					wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
				)
				m, err := wasm.DecodeModule(source)
				if err != nil {
					t.Fatal(err)
				}
				if err := wasm.ValidateModule(m); err != nil {
					t.Fatal(err)
				}
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operation {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operation)
				}
				if len(f.Memory) != 1 || f.Memory[0].SemanticWidth != scalarMemoryWidth(operation) || f.Memory[0].EncodedWidth != f.Memory[0].SemanticWidth {
					t.Fatalf("instruction %d memory descriptor = %#v", index, f.Memory)
				}
			}
		})
	}
}

func TestSelectTargetOpcodesScalarConstants(t *testing.T) {
	operations := [4]MOpcode{wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrF32Const, wasm.InstrF64Const}
	bodies := [4][]byte{
		{0x41, 0x07, 0x0b},
		{0x42, 0x07, 0x0b},
		{0x43, 0, 0, 0, 0, 0x0b},
		{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b},
	}
	types := [4]wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
	for _, test := range []struct {
		name   string
		target Target
		want   [4]MOpcode
	}{
		{"amd64", TargetAMD64, [4]MOpcode{OpAMD64I32Const, OpAMD64I64Const, OpAMD64F32Const, OpAMD64F64Const}},
		{"arm64", TargetARM64, [4]MOpcode{OpARM64I32Const, OpARM64I64Const, OpARM64F32Const, OpARM64F64Const}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, operation := range operations {
				f := buildMachineTest(t, test.target, machineModule(nil, []wasm.ValType{types[index]}, bodies[index]))
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operation {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operation)
				}
			}
		})
	}
}

func TestSelectTargetOpcodesGlobals(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 0x01, 0x41, 0x00, 0x0b})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x00, 0x23, 0x00, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		target Target
		want   [2]MOpcode
	}{
		{"amd64", TargetAMD64, [2]MOpcode{OpAMD64GlobalSet, OpAMD64GlobalGet}},
		{"arm64", TargetARM64, [2]MOpcode{OpARM64GlobalSet, OpARM64GlobalGet}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := buildMachineTest(t, test.target, m)
			count, err := SelectTargetOpcodes(f)
			if err != nil {
				t.Fatal(err)
			}
			if count != 2 || len(f.Insts) != 2 || f.Insts[0].Op != test.want[0] || f.Insts[1].Op != test.want[1] {
				t.Fatalf("selected instructions = %#v, count %d, want %v", f.Insts, count, test.want)
			}
			if SemanticOpcode(f.Insts[0].Op) != wasm.InstrGlobalSet || SemanticOpcode(f.Insts[1].Op) != wasm.InstrGlobalGet {
				t.Fatalf("semantic instructions = %#v", f.Insts)
			}
		})
	}
}

func TestSelectTargetOpcodesSelect(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x1b, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		target Target
		want   MOpcode
	}{
		{"amd64", TargetAMD64, OpAMD64Select},
		{"arm64", TargetARM64, OpARM64Select},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := buildMachineTest(t, test.target, m)
			count, err := SelectTargetOpcodes(f)
			if err != nil {
				t.Fatal(err)
			}
			if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want {
				t.Fatalf("selected instructions = %#v, count %d, want %v", f.Insts, count, test.want)
			}
			if SemanticOpcode(f.Insts[0].Op) != wasm.InstrSelect {
				t.Fatalf("semantic instruction = %#v", f.Insts[0])
			}
		})
	}
}

func TestSelectTargetOpcodesMemoryManagement(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x00, 0x1a, 0x3f, 0x00, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		target Target
		want   [2]MOpcode
	}{
		{"amd64", TargetAMD64, [2]MOpcode{OpAMD64MemorySize, OpAMD64MemoryGrow}},
		{"arm64", TargetARM64, [2]MOpcode{OpARM64MemorySize, OpARM64MemoryGrow}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := buildMachineTest(t, test.target, m)
			count, err := SelectTargetOpcodes(f)
			if err != nil {
				t.Fatal(err)
			}
			if count != 2 || len(f.Insts) != 2 || f.Insts[0].Op != test.want[1] || f.Insts[1].Op != test.want[0] {
				t.Fatalf("selected instructions = %#v, count %d, want %v", f.Insts, count, test.want)
			}
			if SemanticOpcode(f.Insts[0].Op) != wasm.InstrMemoryGrow || SemanticOpcode(f.Insts[1].Op) != wasm.InstrMemorySize {
				t.Fatalf("semantic instructions = %#v", f.Insts)
			}
		})
	}
}

func TestSelectTargetOpcodesBulkMemory(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x00, 0x00,
			0x20, 0x03, 0x20, 0x04, 0x20, 0x05, 0xfc, 0x0b, 0x00, 0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		target Target
		want   [2]MOpcode
	}{
		{"amd64", TargetAMD64, [2]MOpcode{OpAMD64MemoryCopy, OpAMD64MemoryFill}},
		{"arm64", TargetARM64, [2]MOpcode{OpARM64MemoryCopy, OpARM64MemoryFill}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := buildMachineTest(t, test.target, m)
			count, err := SelectTargetOpcodes(f)
			if err != nil {
				t.Fatal(err)
			}
			if count != 2 || len(f.Insts) != 2 || f.Insts[0].Op != test.want[0] || f.Insts[1].Op != test.want[1] {
				t.Fatalf("selected instructions = %#v, count %d, want %v", f.Insts, count, test.want)
			}
			if SemanticOpcode(f.Insts[0].Op) != wasm.InstrMemoryCopy || SemanticOpcode(f.Insts[1].Op) != wasm.InstrMemoryFill {
				t.Fatalf("semantic instructions = %#v", f.Insts)
			}
		})
	}
}

func TestSelectTargetOpcodesControl(t *testing.T) {
	tests := []struct {
		name   string
		params []wasm.ValType
		body   []byte
		kind   MOpcode
		amd64  MOpcode
		arm64  MOpcode
	}{
		{"if", []wasm.ValType{wasm.I32}, []byte{0x20, 0x00, 0x04, 0x40, 0x0b, 0x0b}, wasm.InstrIf, OpAMD64If, OpARM64If},
		{"br", nil, []byte{0x02, 0x40, 0x0c, 0x00, 0x0b, 0x0b}, wasm.InstrBr, OpAMD64Br, OpARM64Br},
		{"br_if", []wasm.ValType{wasm.I32}, []byte{0x02, 0x40, 0x20, 0x00, 0x0d, 0x00, 0x0b, 0x0b}, wasm.InstrBrIf, OpAMD64BrIf, OpARM64BrIf},
		{"br_table", []wasm.ValType{wasm.I32}, []byte{0x02, 0x40, 0x20, 0x00, 0x0e, 0x01, 0x00, 0x00, 0x0b, 0x0b}, wasm.InstrBrTable, OpAMD64BrTable, OpARM64BrTable},
		{"return", nil, []byte{0x0f, 0x0b}, wasm.InstrReturn, OpAMD64Return, OpARM64Return},
		{"unreachable", nil, []byte{0x00, 0x0b}, wasm.InstrUnreachable, OpAMD64Unreachable, OpARM64Unreachable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := machineModule(test.params, nil, test.body)
			for _, target := range []struct {
				name string
				id   Target
				want MOpcode
			}{{"amd64", TargetAMD64, test.amd64}, {"arm64", TargetARM64, test.arm64}} {
				t.Run(target.name, func(t *testing.T) {
					f := buildMachineTest(t, target.id, m)
					if _, err := SelectTargetOpcodes(f); err != nil {
						t.Fatal(err)
					}
					found := false
					for _, instruction := range f.Insts {
						if instruction.Op == target.want && SemanticOpcode(instruction.Op) == test.kind {
							found = true
						}
					}
					if !found {
						t.Fatalf("selected instructions = %#v, want %v", f.Insts, target.want)
					}
				})
			}
		})
	}
}

func TestSelectTargetOpcodesCalls(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		target   Target
		direct   MOpcode
		indirect MOpcode
	}{
		{"amd64", TargetAMD64, OpAMD64Call, OpAMD64CallIndirect},
		{"arm64", TargetARM64, OpARM64Call, OpARM64CallIndirect},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, operation := range []struct {
				name     string
				generic  MOpcode
				selected MOpcode
			}{{"direct", wasm.InstrCall, test.direct}, {"indirect", wasm.InstrCallIndirect, test.indirect}} {
				t.Run(operation.name, func(t *testing.T) {
					f := buildMachineTest(t, test.target, m)
					call := -1
					for index := range f.Insts {
						if f.Insts[index].Op == wasm.InstrCall {
							call = index
							break
						}
					}
					if call < 0 {
						t.Fatal("fixture contains no call")
					}
					f.Insts[call].Op = operation.generic
					if _, err := SelectTargetOpcodes(f); err != nil {
						t.Fatal(err)
					}
					instruction := f.Insts[call]
					if instruction.Op != operation.selected || SemanticOpcode(instruction.Op) != operation.generic || !IsCall(instruction.Op) || instruction.ResultCount() != 1 {
						t.Fatalf("selected call = %#v, want %v with one result", instruction, operation.selected)
					}
				})
			}
		})
	}
}

func TestSelectTargetOpcodesReferencePrimitives(t *testing.T) {
	m := machineModule(nil, nil, []byte{
		0xd0, 0x6d, 0xd1, 0x1a,
		0xd0, 0x6d, 0xd0, 0x6d, 0xd3, 0x1a,
		0xd0, 0x6d, 0xd4, 0x1a,
		0x0b,
	})
	for _, test := range []struct {
		name   string
		target Target
		want   [4]MOpcode
	}{
		{"amd64", TargetAMD64, [4]MOpcode{OpAMD64RefNull, OpAMD64RefIsNull, OpAMD64RefEq, OpAMD64RefAsNonNull}},
		{"arm64", TargetARM64, [4]MOpcode{OpARM64RefNull, OpARM64RefIsNull, OpARM64RefEq, OpARM64RefAsNonNull}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := buildMachineTest(t, test.target, m)
			if _, err := SelectTargetOpcodes(f); err != nil {
				t.Fatal(err)
			}
			semantics := [4]MOpcode{wasm.InstrRefNull, wasm.InstrRefIsNull, wasm.InstrRefEq, wasm.InstrRefAsNonNull}
			for index, want := range test.want {
				found := false
				for _, instruction := range f.Insts {
					if instruction.Op == want && SemanticOpcode(instruction.Op) == semantics[index] {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("selected instructions = %#v, want %v", f.Insts, want)
				}
			}
		})
	}
}

func TestSelectTargetOpcodesI31Primitives(t *testing.T) {
	operations := []struct {
		generic MOpcode
		input   wasm.ValType
		result  wasm.ValType
		body    []byte
		amd64   MOpcode
		arm64   MOpcode
	}{
		{wasm.InstrRefI31, wasm.I32, wasm.I31Ref, []byte{0x20, 0, 0xfb, 0x1c, 0x0b}, OpAMD64RefI31, OpARM64RefI31},
		{wasm.InstrI31GetS, wasm.I31Ref, wasm.I32, []byte{0x20, 0, 0xfb, 0x1d, 0x0b}, OpAMD64I31GetS, OpARM64I31GetS},
		{wasm.InstrI31GetU, wasm.I31Ref, wasm.I32, []byte{0x20, 0, 0xfb, 0x1e, 0x0b}, OpAMD64I31GetU, OpARM64I31GetU},
	}
	for _, target := range []Target{TargetAMD64, TargetARM64} {
		t.Run(target.String(), func(t *testing.T) {
			for _, operation := range operations {
				f := buildMachineTest(t, target, machineModule([]wasm.ValType{operation.input}, []wasm.ValType{operation.result}, operation.body))
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				want := operation.amd64
				if target == TargetARM64 {
					want = operation.arm64
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != want || SemanticOpcode(f.Insts[0].Op) != operation.generic {
					t.Fatalf("%s selected instructions = %#v, count %d, want %d", operation.generic, f.Insts, count, want)
				}
			}
		})
	}
}

func TestSelectTargetOpcodesExternConversions(t *testing.T) {
	operations := []struct {
		generic MOpcode
		input   wasm.ValType
		result  wasm.ValType
		subop   byte
		amd64   MOpcode
		arm64   MOpcode
	}{
		{wasm.InstrAnyConvertExtern, wasm.ExternRef, wasm.AnyRef, 0x1a, OpAMD64AnyConvertExtern, OpARM64AnyConvertExtern},
		{wasm.InstrExternConvertAny, wasm.AnyRef, wasm.ExternRef, 0x1b, OpAMD64ExternConvertAny, OpARM64ExternConvertAny},
	}
	for _, target := range []Target{TargetAMD64, TargetARM64} {
		t.Run(target.String(), func(t *testing.T) {
			for _, operation := range operations {
				body := []byte{0x20, 0, 0xfb, operation.subop, 0x0b}
				f := buildMachineTest(t, target, machineModule([]wasm.ValType{operation.input}, []wasm.ValType{operation.result}, body))
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				want := operation.amd64
				if target == TargetARM64 {
					want = operation.arm64
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != want || SemanticOpcode(f.Insts[0].Op) != operation.generic || !IsCall(f.Insts[0].Op) {
					t.Fatalf("%s selected instructions = %#v, count %d, want helper call %d", operation.generic, f.Insts, count, want)
				}
			}
		})
	}
}

func TestSelectTargetOpcodesReferenceTypeHelpers(t *testing.T) {
	decodeBranchModule := func(blockResult wasm.ValType, body []byte) *wasm.Module {
		t.Helper()
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x00},
				wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32}),
				wasmtest.FuncType(nil, []wasm.ValType{blockResult}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	tests := []struct {
		name    string
		generic MOpcode
		module  *wasm.Module
		amd64   MOpcode
		arm64   MOpcode
	}{
		{"ref_test", wasm.InstrRefTest, machineModule(nil, []wasm.ValType{wasm.I32}, []byte{0x41, 7, 0xfb, 0x1c, 0xfb, 0x14, 0x6c, 0x0b}), OpAMD64RefTest, OpARM64RefTest},
		{"ref_cast", wasm.InstrRefCast, machineModule(nil, []wasm.ValType{wasm.I32}, []byte{0x41, 7, 0xfb, 0x1c, 0xfb, 0x16, 0x6c, 0xfb, 0x1e, 0x0b}), OpAMD64RefCast, OpARM64RefCast},
		{"br_on_cast", wasm.InstrBrOnCast, decodeBranchModule(wasm.EqRef, []byte{0x02, 0x02, 0x20, 0, 0xfb, 0x18, 0x03, 0, 0x6e, 0x6d, 0, 0x0b, 0x1a, 0x41, 1, 0x0b}), OpAMD64BrOnCast, OpARM64BrOnCast},
		{"br_on_cast_fail", wasm.InstrBrOnCastFail, decodeBranchModule(wasm.AnyRef, []byte{0x02, 0x02, 0x20, 0, 0xfb, 0x19, 0x01, 0, 0x6e, 0x6d, 0, 0x0b, 0x1a, 0x41, 1, 0x0b}), OpAMD64BrOnCastFail, OpARM64BrOnCastFail},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, target := range []Target{TargetAMD64, TargetARM64} {
				t.Run(target.String(), func(t *testing.T) {
					f := buildMachineTest(t, target, test.module)
					if _, err := SelectTargetOpcodes(f); err != nil {
						t.Fatal(err)
					}
					want := test.amd64
					if target == TargetARM64 {
						want = test.arm64
					}
					found := false
					for _, instruction := range f.Insts {
						if instruction.Op == want && SemanticOpcode(instruction.Op) == test.generic && IsCall(instruction.Op) {
							found = true
						}
					}
					if !found {
						t.Fatalf("selected instructions = %#v, want helper call %d", f.Insts, want)
					}
				})
			}
		})
	}
}

func TestSelectTargetOpcodesStructFieldHelpers(t *testing.T) {
	structModule := func(fieldType byte, mutable bool, body []byte) *wasm.Module {
		t.Helper()
		mutability := byte(0)
		if mutable {
			mutability = 1
		}
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x01, fieldType, mutability},
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	tests := []struct {
		name    string
		generic MOpcode
		module  *wasm.Module
		amd64   MOpcode
		arm64   MOpcode
	}{
		{"get", wasm.InstrStructGet, structModule(0x7f, false, []byte{0xfb, 0x01, 0, 0xfb, 0x02, 0, 0, 0x0b}), OpAMD64StructGet, OpARM64StructGet},
		{"get_s", wasm.InstrStructGetS, structModule(0x78, false, []byte{0xfb, 0x01, 0, 0xfb, 0x03, 0, 0, 0x0b}), OpAMD64StructGetS, OpARM64StructGetS},
		{"get_u", wasm.InstrStructGetU, structModule(0x78, false, []byte{0xfb, 0x01, 0, 0xfb, 0x04, 0, 0, 0x0b}), OpAMD64StructGetU, OpARM64StructGetU},
		{"set", wasm.InstrStructSet, structModule(0x7f, true, []byte{0xfb, 0x01, 0, 0x41, 1, 0xfb, 0x05, 0, 0, 0x41, 7, 0x0b}), OpAMD64StructSet, OpARM64StructSet},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, target := range []Target{TargetAMD64, TargetARM64} {
				t.Run(target.String(), func(t *testing.T) {
					f := buildMachineTest(t, target, test.module)
					if _, err := SelectTargetOpcodes(f); err != nil {
						t.Fatal(err)
					}
					want := test.amd64
					if target == TargetARM64 {
						want = test.arm64
					}
					found := false
					for _, instruction := range f.Insts {
						if instruction.Op == want && SemanticOpcode(instruction.Op) == test.generic && IsCall(instruction.Op) {
							found = true
						}
					}
					if !found {
						t.Fatalf("selected instructions = %#v, want helper call %d", f.Insts, want)
					}
				})
			}
		})
	}
}

func TestSelectTargetOpcodesStructConstructors(t *testing.T) {
	structModule := func(body []byte) *wasm.Module {
		t.Helper()
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x01, 0x7f, 0x01},
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	tests := []struct {
		name    string
		generic MOpcode
		module  *wasm.Module
		amd64   MOpcode
		arm64   MOpcode
	}{
		{"new", wasm.InstrStructNew, structModule([]byte{0x41, 7, 0xfb, 0x00, 0, 0x1a, 0x41, 1, 0x0b}), OpAMD64StructNew, OpARM64StructNew},
		{"new_default", wasm.InstrStructNewDefault, structModule([]byte{0xfb, 0x01, 0, 0x1a, 0x41, 1, 0x0b}), OpAMD64StructNewDefault, OpARM64StructNewDefault},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, target := range []Target{TargetAMD64, TargetARM64} {
				t.Run(target.String(), func(t *testing.T) {
					f := buildMachineTest(t, target, test.module)
					if _, err := SelectTargetOpcodes(f); err != nil {
						t.Fatal(err)
					}
					want := test.amd64
					if target == TargetARM64 {
						want = test.arm64
					}
					found := false
					for _, instruction := range f.Insts {
						if instruction.Op == want && SemanticOpcode(instruction.Op) == test.generic && IsCall(instruction.Op) {
							found = true
						}
					}
					if !found {
						t.Fatalf("selected instructions = %#v, want helper call %d", f.Insts, want)
					}
				})
			}
		})
	}
}

func TestSelectTargetOpcodesArrayFieldHelpers(t *testing.T) {
	arrayModule := func(fieldType byte, body []byte) *wasm.Module {
		t.Helper()
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5e, fieldType, 0x01},
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	tests := []struct {
		name    string
		generic MOpcode
		module  *wasm.Module
		amd64   MOpcode
		arm64   MOpcode
	}{
		{"get", wasm.InstrArrayGet, arrayModule(0x7f, []byte{0x41, 1, 0xfb, 0x07, 0, 0x41, 0, 0xfb, 0x0b, 0, 0x0b}), OpAMD64ArrayGet, OpARM64ArrayGet},
		{"get_s", wasm.InstrArrayGetS, arrayModule(0x78, []byte{0x41, 1, 0xfb, 0x07, 0, 0x41, 0, 0xfb, 0x0c, 0, 0x0b}), OpAMD64ArrayGetS, OpARM64ArrayGetS},
		{"get_u", wasm.InstrArrayGetU, arrayModule(0x78, []byte{0x41, 1, 0xfb, 0x07, 0, 0x41, 0, 0xfb, 0x0d, 0, 0x0b}), OpAMD64ArrayGetU, OpARM64ArrayGetU},
		{"set", wasm.InstrArraySet, arrayModule(0x7f, []byte{0x41, 1, 0xfb, 0x07, 0, 0x41, 0, 0x41, 7, 0xfb, 0x0e, 0, 0x41, 1, 0x0b}), OpAMD64ArraySet, OpARM64ArraySet},
		{"len", wasm.InstrArrayLen, arrayModule(0x7f, []byte{0x41, 1, 0xfb, 0x07, 0, 0xfb, 0x0f, 0x0b}), OpAMD64ArrayLen, OpARM64ArrayLen},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, target := range []Target{TargetAMD64, TargetARM64} {
				t.Run(target.String(), func(t *testing.T) {
					f := buildMachineTest(t, target, test.module)
					if _, err := SelectTargetOpcodes(f); err != nil {
						t.Fatal(err)
					}
					want := test.amd64
					if target == TargetARM64 {
						want = test.arm64
					}
					found := false
					for _, instruction := range f.Insts {
						if instruction.Op == want && SemanticOpcode(instruction.Op) == test.generic && IsCall(instruction.Op) {
							found = true
						}
					}
					if !found {
						t.Fatalf("selected instructions = %#v, want helper call %d", f.Insts, want)
					}
				})
			}
		})
	}
}

func TestSelectTargetOpcodesArrayConstructors(t *testing.T) {
	arrayModule := func(arrayType, body []byte, beforeCode, afterCode [][]byte) *wasm.Module {
		t.Helper()
		sections := [][]byte{
			wasmtest.Section(1, wasmtest.Vec(arrayType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		}
		sections = append(sections, beforeCode...)
		sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		sections = append(sections, afterCode...)
		m, err := wasm.DecodeModule(wasmtest.Module(sections...))
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	passiveData := append([]byte{0x01}, append(wasmtest.ULEB(1), 'x')...)
	passiveElem := append([]byte{0x05, 0x70}, wasmtest.ULEB(0)...)
	tests := []struct {
		name    string
		generic MOpcode
		module  *wasm.Module
		amd64   MOpcode
		arm64   MOpcode
	}{
		{"new", wasm.InstrArrayNew, arrayModule([]byte{0x5e, 0x7e, 0x01}, []byte{0x42, 7, 0x41, 2, 0xfb, 0x06, 0, 0x1a, 0x41, 1, 0x0b}, nil, nil), OpAMD64ArrayNew, OpARM64ArrayNew},
		{"new_default", wasm.InstrArrayNewDefault, arrayModule([]byte{0x5e, 0x7e, 0x01}, []byte{0x41, 2, 0xfb, 0x07, 0, 0x1a, 0x41, 1, 0x0b}, nil, nil), OpAMD64ArrayNewDefault, OpARM64ArrayNewDefault},
		{"new_fixed", wasm.InstrArrayNewFixed, arrayModule([]byte{0x5e, 0x7e, 0x01}, []byte{0x42, 1, 0x42, 2, 0xfb, 0x08, 0, 2, 0x1a, 0x41, 1, 0x0b}, nil, nil), OpAMD64ArrayNewFixed, OpARM64ArrayNewFixed},
		{"new_data", wasm.InstrArrayNewData, arrayModule([]byte{0x5e, 0x78, 0x01}, []byte{0x41, 0, 0x41, 1, 0xfb, 0x09, 0, 0, 0x1a, 0x41, 1, 0x0b}, [][]byte{wasmtest.Section(12, wasmtest.ULEB(1))}, [][]byte{wasmtest.Section(11, wasmtest.Vec(passiveData))}), OpAMD64ArrayNewData, OpARM64ArrayNewData},
		{"new_elem", wasm.InstrArrayNewElem, arrayModule([]byte{0x5e, 0x70, 0x01}, []byte{0x41, 0, 0x41, 0, 0xfb, 0x0a, 0, 0, 0x1a, 0x41, 1, 0x0b}, [][]byte{wasmtest.Section(9, wasmtest.Vec(passiveElem))}, nil), OpAMD64ArrayNewElem, OpARM64ArrayNewElem},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, target := range []Target{TargetAMD64, TargetARM64} {
				t.Run(target.String(), func(t *testing.T) {
					f := buildMachineTest(t, target, test.module)
					if _, err := SelectTargetOpcodes(f); err != nil {
						t.Fatal(err)
					}
					want := test.amd64
					if target == TargetARM64 {
						want = test.arm64
					}
					found := false
					for _, instruction := range f.Insts {
						if instruction.Op == want && SemanticOpcode(instruction.Op) == test.generic && IsCall(instruction.Op) {
							found = true
						}
					}
					if !found {
						t.Fatalf("selected instructions = %#v, want helper call %d", f.Insts, want)
					}
				})
			}
		})
	}
}

func TestSelectTargetOpcodesArrayLifecycleHelpers(t *testing.T) {
	arrayModule := func(arrayType, body []byte, beforeCode, afterCode [][]byte) *wasm.Module {
		t.Helper()
		sections := [][]byte{
			wasmtest.Section(1, wasmtest.Vec(arrayType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		}
		sections = append(sections, beforeCode...)
		sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		sections = append(sections, afterCode...)
		m, err := wasm.DecodeModule(wasmtest.Module(sections...))
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	passiveData := append([]byte{0x01}, append(wasmtest.ULEB(1), 'x')...)
	passiveElem := append([]byte{0x05, 0x70}, wasmtest.ULEB(0)...)
	tests := []struct {
		name    string
		generic MOpcode
		module  *wasm.Module
		amd64   MOpcode
		arm64   MOpcode
		call    bool
	}{
		{"fill", wasm.InstrArrayFill, arrayModule([]byte{0x5e, 0x7f, 0x01}, []byte{0x41, 1, 0xfb, 0x07, 0, 0x41, 0, 0x41, 7, 0x41, 1, 0xfb, 0x10, 0, 0x41, 1, 0x0b}, nil, nil), OpAMD64ArrayFill, OpARM64ArrayFill, true},
		{"copy", wasm.InstrArrayCopy, arrayModule([]byte{0x5e, 0x7f, 0x01}, []byte{0x41, 1, 0xfb, 0x07, 0, 0x41, 0, 0x41, 1, 0xfb, 0x07, 0, 0x41, 0, 0x41, 1, 0xfb, 0x11, 0, 0, 0x41, 1, 0x0b}, nil, nil), OpAMD64ArrayCopy, OpARM64ArrayCopy, true},
		{"init_data", wasm.InstrArrayInitData, arrayModule([]byte{0x5e, 0x78, 0x01}, []byte{0x41, 1, 0xfb, 0x07, 0, 0x41, 0, 0x41, 0, 0x41, 1, 0xfb, 0x12, 0, 0, 0x41, 1, 0x0b}, [][]byte{wasmtest.Section(12, wasmtest.ULEB(1))}, [][]byte{wasmtest.Section(11, wasmtest.Vec(passiveData))}), OpAMD64ArrayInitData, OpARM64ArrayInitData, true},
		{"init_elem", wasm.InstrArrayInitElem, arrayModule([]byte{0x5e, 0x70, 0x01}, []byte{0x41, 0, 0xfb, 0x07, 0, 0x41, 0, 0x41, 0, 0x41, 0, 0xfb, 0x13, 0, 0, 0x41, 1, 0x0b}, [][]byte{wasmtest.Section(9, wasmtest.Vec(passiveElem))}, nil), OpAMD64ArrayInitElem, OpARM64ArrayInitElem, true},
		{"data_drop", wasm.InstrDataDrop, arrayModule([]byte{0x5e, 0x78, 0x01}, []byte{0xfc, 0x09, 0, 0x41, 1, 0x0b}, [][]byte{wasmtest.Section(12, wasmtest.ULEB(1))}, [][]byte{wasmtest.Section(11, wasmtest.Vec(passiveData))}), OpAMD64DataDrop, OpARM64DataDrop, false},
		{"elem_drop", wasm.InstrElemDrop, arrayModule([]byte{0x5e, 0x70, 0x01}, []byte{0xfc, 0x0d, 0, 0x41, 1, 0x0b}, [][]byte{wasmtest.Section(9, wasmtest.Vec(passiveElem))}, nil), OpAMD64ElemDrop, OpARM64ElemDrop, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, target := range []Target{TargetAMD64, TargetARM64} {
				t.Run(target.String(), func(t *testing.T) {
					f := buildMachineTest(t, target, test.module)
					if _, err := SelectTargetOpcodes(f); err != nil {
						t.Fatal(err)
					}
					want := test.amd64
					if target == TargetARM64 {
						want = test.arm64
					}
					found := false
					for _, instruction := range f.Insts {
						if instruction.Op == want && SemanticOpcode(instruction.Op) == test.generic && IsCall(instruction.Op) == test.call {
							found = true
						}
					}
					if !found {
						t.Fatalf("selected instructions = %#v, want opcode %d (call=%t)", f.Insts, want, test.call)
					}
				})
			}
		})
	}
}

func TestSelectTargetOpcodesRefFunc(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("target", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xd2, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		target Target
		want   MOpcode
	}{
		{"amd64", TargetAMD64, OpAMD64RefFunc},
		{"arm64", TargetARM64, OpARM64RefFunc},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := buildMachineTest(t, test.target, m)
			if _, err := SelectTargetOpcodes(f); err != nil {
				t.Fatal(err)
			}
			if len(f.Insts) != 1 || f.Insts[0].Op != test.want || SemanticOpcode(f.Insts[0].Op) != wasm.InstrRefFunc {
				t.Fatalf("selected instructions = %#v, want %v", f.Insts, test.want)
			}
		})
	}
}

func TestSelectTargetOpcodesIntegerComparisons(t *testing.T) {
	operations := [22]MOpcode{
		wasm.InstrI32Eqz, wasm.InstrI64Eqz,
		wasm.InstrI32Eq, wasm.InstrI64Eq, wasm.InstrI32Ne, wasm.InstrI64Ne,
		wasm.InstrI32LtS, wasm.InstrI64LtS, wasm.InstrI32LtU, wasm.InstrI64LtU,
		wasm.InstrI32GtS, wasm.InstrI64GtS, wasm.InstrI32GtU, wasm.InstrI64GtU,
		wasm.InstrI32LeS, wasm.InstrI64LeS, wasm.InstrI32LeU, wasm.InstrI64LeU,
		wasm.InstrI32GeS, wasm.InstrI64GeS, wasm.InstrI32GeU, wasm.InstrI64GeU,
	}
	encodings := [22]byte{
		0x45, 0x50,
		0x46, 0x51, 0x47, 0x52,
		0x48, 0x53, 0x49, 0x54,
		0x4a, 0x55, 0x4b, 0x56,
		0x4c, 0x57, 0x4d, 0x58,
		0x4e, 0x59, 0x4f, 0x5a,
	}
	for _, test := range []struct {
		name   string
		target Target
		want   [22]MOpcode
	}{
		{"amd64", TargetAMD64, [22]MOpcode{
			OpAMD64I32Eqz, OpAMD64I64Eqz,
			OpAMD64I32Eq, OpAMD64I64Eq, OpAMD64I32Ne, OpAMD64I64Ne,
			OpAMD64I32LtS, OpAMD64I64LtS, OpAMD64I32LtU, OpAMD64I64LtU,
			OpAMD64I32GtS, OpAMD64I64GtS, OpAMD64I32GtU, OpAMD64I64GtU,
			OpAMD64I32LeS, OpAMD64I64LeS, OpAMD64I32LeU, OpAMD64I64LeU,
			OpAMD64I32GeS, OpAMD64I64GeS, OpAMD64I32GeU, OpAMD64I64GeU,
		}},
		{"arm64", TargetARM64, [22]MOpcode{
			OpARM64I32Eqz, OpARM64I64Eqz,
			OpARM64I32Eq, OpARM64I64Eq, OpARM64I32Ne, OpARM64I64Ne,
			OpARM64I32LtS, OpARM64I64LtS, OpARM64I32LtU, OpARM64I64LtU,
			OpARM64I32GtS, OpARM64I64GtS, OpARM64I32GtU, OpARM64I64GtU,
			OpARM64I32LeS, OpARM64I64LeS, OpARM64I32LeU, OpARM64I64LeU,
			OpARM64I32GeS, OpARM64I64GeS, OpARM64I32GeU, OpARM64I64GeU,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, encoding := range encodings {
				type_ := wasm.I32
				if index&1 != 0 {
					type_ = wasm.I64
				}
				params := []wasm.ValType{type_}
				body := []byte{0x20, 0, encoding, 0x0b}
				if index >= 2 {
					params = append(params, type_)
					body = []byte{0x20, 0, 0x20, 1, encoding, 0x0b}
				}
				f := buildMachineTest(t, test.target, machineModule(params, []wasm.ValType{wasm.I32}, body))
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != operations[index] {
					t.Fatalf("instruction %d semantic opcode = %d, want %d", index, got, operations[index])
				}
			}
		})
	}
}

func vectorFoundationFixture(target Target) *Func {
	f := &Func{
		Target: target,
		VRegs:  []VRegData{{}, {Type: TypeI32, Bank: BankGPR}, {Type: TypeV128, Bank: BankFPR}, {Type: TypeV128, Bank: BankFPR}, {Type: TypeV128, Bank: BankFPR}},
		Insts: []Inst{
			{Op: wasm.InstrV128Const, Result: 2, Source: 1},
			{Op: wasm.InstrV128Load, Result: 3, Source: 2, Aux: 8, OperandStart: 0, OperandCount: 1},
			{Op: wasm.InstrV128And, Result: 4, Source: 3, OperandStart: 1, OperandCount: 2},
			{Op: wasm.InstrV128Store, Source: 4, Aux: 24, OperandStart: 3, OperandCount: 2},
		},
		Operands: []Operand{
			{Reg: 1, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse},
			{Reg: 2, Fixed: NoFixedReg, Bank: BankFPR, Flags: OperandUse}, {Reg: 3, Fixed: NoFixedReg, Bank: BankFPR, Flags: OperandUse},
			{Reg: 1, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse}, {Reg: 4, Fixed: NoFixedReg, Bank: BankFPR, Flags: OperandUse},
		},
		Blocks: []Block{{InstCount: 4}},
		Memory: []MemoryAccess{
			{Instruction: 1, AddressValue: 1, Offset: 8, TrapSite: 2, SemanticWidth: 16, EncodedWidth: 16, Alignment: 1},
			{Instruction: 3, AddressValue: 1, Offset: 24, TrapSite: 4, SemanticWidth: 16, EncodedWidth: 16, Alignment: 1},
		},
	}
	for index := range f.Insts {
		f.SIMD = append(f.SIMD, railssa.SemanticSIMDImmediate{Instruction: uint32(index)})
	}
	return f
}

func TestVerifyMemoryAccessIdentityAndWidth(t *testing.T) {
	f := memoryFixture()
	if err := Verify(f); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		edit func(*Func)
		want string
	}{
		{"widened access", func(f *Func) { f.Memory[0].EncodedWidth = 16 }, "touches 16 bytes"},
		{"different address", func(f *Func) { f.Memory[0].AddressValue = 2 }, "different address"},
		{"different offset", func(f *Func) { f.Memory[0].Offset++ }, "different constant offset"},
		{"different trap", func(f *Func) { f.Memory[0].TrapSite++ }, "different trap source"},
		{"invalid alignment", func(f *Func) { f.Memory[0].Alignment = 3 }, "invalid 3-byte alignment"},
		{"invalid bounds proof", func(f *Func) { f.BoundsProofCount, f.Memory[0].BoundsProof = 1, 2 }, "invalid bounds proof"},
		{"dropped bounds proof", func(f *Func) { f.BoundsProofCount = 1 }, "preserved 0 of 1 bounds proofs"},
		{"missing descriptor", func(f *Func) { f.Memory = nil }, "no access descriptor"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := memoryFixture()
			test.edit(candidate)
			if err := Verify(candidate); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Verify = %v, want %q", err, test.want)
			}
		})
	}
}

func TestBindBoundsProofsMakesMachineAccessAuthoritative(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x28, 0x02, 0x00, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	var planner railssa.EmissionPlanner
	emission, err := planner.Plan(stack)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := railssa.BuildCFG(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	locals, err := railssa.BuildLocalSSA(stack, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := railssa.BuildValueFlow(stack, cfg, locals, nil)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := railssa.BuildSemanticFunc(stack, cfg, flow, nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := railssa.BuildMetadata(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	simplified, err := railssa.SparseSimplify(stack, cfg, flow, semantic, metadata, railssa.DefaultSimplifyConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := BuildWithSimplify(TargetARM64, cfg, flow, semantic, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := BindBoundsProofs(machine, emission); err != nil {
		t.Fatal(err)
	}
	if machine.BoundsProofCount != 1 || len(machine.Memory) != 1 || machine.Memory[0].BoundsProof != 1 {
		t.Fatalf("machine bounds proofs = %d %#v", machine.BoundsProofCount, machine.Memory)
	}
	machine.Memory[0].BoundsProof = 0
	if err := Verify(machine); err == nil || !strings.Contains(err.Error(), "preserved 0 of 1 bounds proofs") {
		t.Fatalf("Verify dropped proof = %v", err)
	}
}

func memoryFixture() *Func {
	return &Func{
		Target:   TargetARM64,
		VRegs:    []VRegData{{}, {Type: TypeI32, Bank: BankGPR}, {Type: TypeI32, Bank: BankGPR}},
		Operands: []Operand{{Reg: 1, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse}},
		Insts:    []Inst{{Op: wasm.InstrI32Load, Aux: 24, Result: 2, OperandCount: 1, Source: 7}},
		Blocks:   []Block{{InstCount: 1}},
		Memory:   []MemoryAccess{{Instruction: 0, AddressValue: 1, Offset: 24, TrapSite: 7, SemanticWidth: 4, EncodedWidth: 4, Alignment: 1}},
	}
}

func TestBuildPreservesV128InFPRBank(t *testing.T) {
	body := []byte{0xfd, 0x0c}
	body = append(body, make([]byte, 16)...)
	body = append(body, 0x0b)
	m := machineModule(nil, []wasm.ValType{wasm.V128}, body)
	for _, target := range []Target{TargetAMD64, TargetARM64} {
		f := buildMachineTest(t, target, m)
		if len(f.SIMD) != 1 || f.SIMD[0].Bytes != [16]byte{} || f.SIMD[0].Class != wasm.SIMDEffectConst {
			t.Fatalf("%s SIMD constant = %#v", target, f.SIMD)
		}
		if len(f.Results) != 1 {
			t.Fatalf("%s results = %#v", target, f.Results)
		}
		result := f.VRegs[f.Results[0]]
		if result.Type != TypeV128 || result.Bank != BankFPR || !result.Type.IsVector() {
			t.Fatalf("%s v128 result = %#v", target, result)
		}
		if err := Verify(f); err != nil {
			t.Fatalf("%s verify: %v", target, err)
		}
	}
}

func TestBuildPreservesSparseSIMDMemorySemantics(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0xfd, 0x07, 0x00, 0x09, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	f := buildMachineTest(t, TargetARM64, m)
	if len(f.SIMD) != 1 || f.SIMD[0].Offset != 9 || f.SIMD[0].Class != wasm.SIMDEffectLoad {
		t.Fatalf("SIMD immediate = %#v", f.SIMD)
	}
	if len(f.Memory) != 1 || f.Memory[0].SemanticWidth != 1 || f.Memory[0].EncodedWidth != 1 || f.Memory[0].Offset != 9 || f.Memory[0].AddressValue == 0 {
		t.Fatalf("SIMD memory access = %#v", f.Memory)
	}
}

func TestVerifyRejectsMachineTypeInWrongBank(t *testing.T) {
	f := &Func{Target: TargetARM64, VRegs: []VRegData{{}, {Type: TypeV128, Bank: BankGPR}}}
	if err := Verify(f); err == nil || !strings.Contains(err.Error(), "invalid type/bank") {
		t.Fatalf("Verify wrong-bank v128 = %v", err)
	}
}

func TestBuildPreservesBlockArgumentsAndSourceOrder(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x04, 0x7f,
		0x41, 0x01,
		0x05,
		0x41, 0x02,
		0x0b,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	if len(f.Transfers) != 2 {
		t.Fatalf("transfers = %#v", f.Transfers)
	}
	if err := ScheduleSourceStable(f); err != nil {
		t.Fatal(err)
	}
	dump := Dump(f)
	if !strings.Contains(dump, "target arm64") || !strings.Contains(dump, "edge") || !strings.Contains(dump, "I32Const") {
		t.Fatalf("dump:\n%s", dump)
	}
}

func TestBuildElidesVerifiedDominatingCrossBlockDefinitions(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00, 0x41, 0x07, 0x6a, 0x1a,
		0x20, 0x00,
		0x04, 0x7f,
		0x20, 0x00, 0x41, 0x07, 0x6a,
		0x05,
		0x41, 0x00,
		0x0b,
		0x0b,
	})
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := railssa.BuildCFG(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	locals, err := railssa.BuildLocalSSA(stack, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := railssa.BuildValueFlow(stack, cfg, locals, nil)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := railssa.BuildSemanticFunc(stack, cfg, flow, nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := railssa.BuildMetadata(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	simplified, err := railssa.SparseSimplify(stack, cfg, flow, semantic, metadata, railssa.DefaultSimplifyConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := BuildWithSimplify(TargetARM64, cfg, flow, semantic, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	var crossBlock int
	for value, alias := range simplified.Aliases {
		canonical := resolveMachineAlias(simplified.Aliases, alias)
		if value == 0 || canonical == railssa.FlowValueID(value) || flow.Values[value].Kind != railssa.FlowValueInstruction || flow.Values[canonical].Kind != railssa.FlowValueInstruction || flow.Values[value].Block == flow.Values[canonical].Block {
			continue
		}
		crossBlock++
		if machine.VRegs[value].Flags&VRegElided == 0 {
			t.Fatalf("dominated cross-block alias v%d -> v%d was retained", value, canonical)
		}
		for instructionID := range machine.Insts {
			for _, operand := range machine.InstructionOperands(uint32(instructionID)) {
				if operand.Reg == VReg(value) {
					t.Fatalf("instruction %d still uses elided cross-block alias v%d", instructionID, value)
				}
			}
		}
	}
	if crossBlock == 0 {
		t.Fatal("fixture produced no cross-block instruction alias")
	}
}

func TestBuildElidesVerifiedTrivialBlockParameters(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x02, 0x40,
		0x03, 0x40,
		0x20, 0x00,
		0x21, 0x00,
		0x20, 0x00,
		0x0d, 0x01,
		0x0c, 0x00,
		0x0b,
		0x0b,
		0x20, 0x00,
		0x0b,
	})
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := railssa.BuildCFG(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	locals, err := railssa.BuildLocalSSA(stack, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := railssa.BuildValueFlow(stack, cfg, locals, nil)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := railssa.BuildSemanticFunc(stack, cfg, flow, nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := railssa.BuildMetadata(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	simplified, err := railssa.SparseSimplify(stack, cfg, flow, semantic, metadata, railssa.DefaultSimplifyConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := BuildWithSimplify(TargetARM64, cfg, flow, semantic, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	elided := 0
	for value, record := range flow.Values {
		if record.Kind != railssa.FlowValueBlockParam || resolveMachineAlias(simplified.Aliases, railssa.FlowValueID(value)) == railssa.FlowValueID(value) {
			continue
		}
		elided++
		if machine.VRegs[value].Flags&VRegElided == 0 {
			t.Fatalf("trivial block parameter v%d was retained", value)
		}
		for _, transfer := range machine.Transfers {
			if transfer.Dst == VReg(value) {
				t.Fatalf("trivial block parameter v%d retains transfer %#v", value, transfer)
			}
		}
	}
	if elided == 0 {
		t.Fatal("fixture produced no trivial block parameter")
	}
}

func TestAMD64ShiftCountIsFixed(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x20, 0x01,
		0x86,
		0x0b,
	})
	amd := buildMachineTest(t, TargetAMD64, m)
	if len(amd.Insts) != 1 {
		t.Fatalf("instructions = %#v", amd.Insts)
	}
	operands := amd.InstructionOperands(0)
	if len(operands) != 2 || operands[1].Flags&OperandFixed == 0 || operands[1].Fixed != 1 {
		t.Fatalf("AMD64 shift operands = %#v", operands)
	}
	arm := buildMachineTest(t, TargetARM64, m)
	if arm.InstructionOperands(0)[1].Flags&OperandFixed != 0 {
		t.Fatalf("ARM64 shift count unexpectedly fixed: %#v", arm.InstructionOperands(0)[1])
	}
}

func TestColdConstantUseDoesNotExtendAllocatedInterval(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x41, 0x07,
		0x20, 0x00,
		0x6a,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	if len(f.Insts) != 2 || f.Insts[0].Op != wasm.InstrI32Const || f.Insts[1].Op != wasm.InstrI32Add {
		t.Fatalf("instructions = %#v", f.Insts)
	}
	value := f.Insts[0].Result
	pressure := &railssa.PressurePlan{
		Remats:   []railssa.RematRecipe{{Value: railssa.FlowValueID(value), Aux: 7, Kind: railssa.RematConstant}},
		ColdUses: []railssa.ColdUse{{Value: railssa.FlowValueID(value), Instruction: 1, HotWeight: 8, ColdWeight: 1}},
	}
	committed, err := ApplyColdRematerialization(f, pressure, nil)
	if err != nil {
		t.Fatal(err)
	}
	if committed != 1 || f.InstructionOperands(1)[0].Flags&OperandColdRemat == 0 {
		t.Fatalf("committed=%d operands=%#v", committed, f.InstructionOperands(1))
	}
	allocation, err := AllocateLinearQ(f, LinearQConfig{GPRs: 2, FPRs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, interval := range allocation.Intervals {
		if interval.Reg == value {
			t.Fatalf("cold-only constant retained interval %#v", interval)
		}
	}
}

func TestColdExtensionAndAffineUsesCommitWhenTargetLegal(t *testing.T) {
	extendModule := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0xac,
		0x42, 0x03,
		0x7c,
		0x0b,
	})
	extend := buildMachineTest(t, TargetARM64, extendModule)
	if selected, err := SelectTargetOpcodes(extend); err != nil || selected != 3 || extend.Insts[0].Op != OpARM64I64ExtendI32S || extend.Insts[1].Op != OpARM64I64Const {
		t.Fatalf("selected extension instructions=%d err=%v instructions=%#v", selected, err, extend.Insts)
	}
	extended := extend.Insts[0].Result
	extendPlan := &railssa.PressurePlan{
		Remats:   []railssa.RematRecipe{{Value: railssa.FlowValueID(extended), Base: railssa.FlowValueID(extend.InstructionOperands(0)[0].Reg), Kind: railssa.RematExtend}},
		ColdUses: []railssa.ColdUse{{Value: railssa.FlowValueID(extended), Instruction: 2, HotWeight: 8, ColdWeight: 1}},
	}
	if committed, err := ApplyColdRematerialization(extend, extendPlan, nil); err != nil || committed != 1 || extend.InstructionOperands(2)[0].Flags&OperandColdRemat == 0 {
		t.Fatalf("extension committed=%d err=%v operands=%#v", committed, err, extend.InstructionOperands(2))
	}

	affineModule := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x42, 0x03,
		0x7c,
		0x42, 0x02,
		0x7e,
		0x0b,
	})
	affine := buildMachineTest(t, TargetARM64, affineModule)
	affineValue := affine.Insts[1].Result
	affinePlan := &railssa.PressurePlan{
		Remats:   []railssa.RematRecipe{{Value: railssa.FlowValueID(affineValue), Base: railssa.FlowValueID(affine.InstructionOperands(1)[0].Reg), Aux: 3, Kind: railssa.RematAffine}},
		ColdUses: []railssa.ColdUse{{Value: railssa.FlowValueID(affineValue), Instruction: 3, HotWeight: 8, ColdWeight: 1}},
	}
	priced := &RematPlan{Decisions: []RematDecision{{Value: affineValue, Base: affine.InstructionOperands(1)[0].Reg, RecipeCost: 2, SpillCost: 20, Profitable: true}}}
	if committed, err := ApplyColdRematerialization(affine, affinePlan, priced); err != nil || committed != 1 || affine.InstructionOperands(3)[0].Flags&OperandColdRemat == 0 {
		t.Fatalf("affine committed=%d err=%v operands=%#v", committed, err, affine.InstructionOperands(3))
	}
}

func TestColdRematerializationRejectsAggregateHotUseCost(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x42, 0x03,
		0x7c,
		0x42, 0x02,
		0x7e,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	affineValue := f.Insts[1].Result
	pressure := &railssa.PressurePlan{
		Remats: []railssa.RematRecipe{{Value: railssa.FlowValueID(affineValue), Base: railssa.FlowValueID(f.InstructionOperands(1)[0].Reg), Aux: 3, Kind: railssa.RematAffine}},
		ColdUses: []railssa.ColdUse{
			{Value: railssa.FlowValueID(affineValue), Instruction: 3, HotWeight: 64, ColdWeight: 16},
			{Value: railssa.FlowValueID(affineValue), Instruction: 3, HotWeight: 64, ColdWeight: 16},
		},
	}
	priced := &RematPlan{Decisions: []RematDecision{{Value: affineValue, Base: f.InstructionOperands(1)[0].Reg, RecipeCost: 2, SpillCost: 20, Profitable: true}}}
	committed, err := ApplyColdRematerialization(f, pressure, priced)
	if err != nil {
		t.Fatal(err)
	}
	if committed != 0 || f.InstructionOperands(3)[0].Flags&OperandColdRemat != 0 {
		t.Fatalf("aggregate-expensive rematerialization committed=%d operands=%#v", committed, f.InstructionOperands(3))
	}
}
