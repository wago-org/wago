package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestPlanPostRAFindsAMD64LEA(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x20, 0x01,
		0x7c,
		0x0b,
	})
	f, selection, _, dag := buildScheduleTest(t, TargetAMD64, m)
	schedule, err := BuildSchedule(f, selection, dag, ScheduleKindLatencyFusion, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := AllocateGreedyP(f, DefaultGreedyConfig(TargetAMD64), nil)
	if err != nil {
		t.Fatal(err)
	}
	exit, err := LateSSAExit(f, &allocation.Allocation, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanPostRA(TargetAMD64, f, selection, schedule, allocation, exit, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, rewrite := range plan.Rewrites {
		found = found || rewrite.Kind == RewriteAMD64LEA
	}
	if !found {
		t.Fatalf("rewrites = %#v", plan.Rewrites)
	}
}

func TestPlanPostRAFindsARM64ConditionalIncrement(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x02, // accumulator
		0x20, 0x00,
		0x20, 0x01,
		0x48, // i32.lt_s
		0x6a, // i32.add
		0x0b,
	})
	f, selection, _, dag := buildScheduleTest(t, TargetARM64, m)
	schedule, err := BuildSchedule(f, selection, dag, ScheduleKindSourceStable, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := AllocateGreedyPForSchedule(f, schedule, DefaultGreedyConfig(TargetARM64), nil)
	if err != nil {
		t.Fatal(err)
	}
	exit, err := LateSSAExit(f, &allocation.Allocation, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanPostRA(TargetARM64, f, selection, schedule, allocation, exit, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, rewrite := range plan.Rewrites {
		if rewrite.Kind == RewriteARM64CondIncrement {
			return
		}
	}
	t.Fatalf("rewrites = %#v", plan.Rewrites)
}

func TestVerifyARM64ByteWidenChain(t *testing.T) {
	var operands []Operand
	inst := func(op wasm.InstrKind, result VReg, args ...VReg) Inst {
		instruction := Inst{Op: op, Result: result, OperandStart: uint32(len(operands)), OperandCount: uint16(len(args))}
		for _, arg := range args {
			operands = append(operands, Operand{Reg: arg, Bank: BankGPR, Flags: OperandUse, Fixed: NoFixedReg})
		}
		return instruction
	}
	f := &Func{Target: TargetARM64, VRegs: make([]VRegData, 14)}
	f.Insts = []Inst{
		inst(wasm.InstrI64And, 3, 1, 2),
		inst(wasm.InstrI64Const, 4),
		inst(wasm.InstrI64Shl, 5, 3, 4),
		inst(wasm.InstrI64Or, 6, 3, 5),
		inst(wasm.InstrI64Const, 7),
		inst(wasm.InstrI64And, 8, 6, 7),
		inst(wasm.InstrI64Const, 9),
		inst(wasm.InstrI64Shl, 10, 8, 9),
		inst(wasm.InstrI64Or, 11, 8, 10),
		inst(wasm.InstrI64Const, 12),
		inst(wasm.InstrI64And, 13, 11, 12),
	}
	f.Operands = operands
	f.Insts[1].Aux = 16
	f.Insts[4].Aux = 0x0000ffff0000ffff
	f.Insts[6].Aux = 8
	f.Insts[9].Aux = 0x00ff00ff00ff00ff
	f.VRegs[1] = VRegData{Type: TypeI64, Bank: BankGPR, Flags: VRegInitial}
	for id, instruction := range f.Insts {
		if instruction.Result != 0 {
			f.VRegs[instruction.Result] = VRegData{Def: uint32(id*6 + 3), Type: TypeI64, Bank: BankGPR}
		}
	}
	// The first mask is defined outside the compact scheduled chain so shared
	// constants do not constrain adjacency.
	f.VRegs[2] = VRegData{Def: uint32(len(f.Insts) * 6), Type: TypeI64, Bank: BankGPR}
	f.Insts = append(f.Insts, Inst{Op: wasm.InstrI64Const, Result: 2, Aux: 0xffffffff})
	f.VRegs[2].Def = uint32((len(f.Insts)-1)*6 + 3)
	f.Results = []VReg{13}
	order := make([]uint32, 11)
	for index := range order {
		order[index] = uint32(index)
	}
	schedule := &Schedule{Order: order, BlockOf: make([]railssa.BlockID, len(f.Insts))}
	if first, source, ok := VerifyARM64ByteWidenChain(f, schedule, 0, 10); !ok || first != 0 || source != 1 {
		t.Fatalf("byte widen = first %d source %d ok %t", first, source, ok)
	}
	f.Insts[9].Aux++
	if _, _, ok := VerifyARM64ByteWidenChain(f, schedule, 0, 10); ok {
		t.Fatal("changed terminal mask was recognized")
	}
}

func TestVerifyPostRAAllowsScheduledAdjacentARM64CompareBeyondSourceScan(t *testing.T) {
	insts := make([]Inst, PostRAScanLimit+2)
	insts[0] = Inst{Op: wasm.InstrI32Eq, Result: 1, OperandStart: 0, OperandCount: 2}
	for index := 1; index < len(insts)-1; index++ {
		insts[index] = Inst{Op: wasm.InstrNop}
	}
	consumer := uint32(len(insts) - 1)
	insts[consumer] = Inst{Op: wasm.InstrIf, OperandStart: 2, OperandCount: 1}
	f := &Func{
		Target: TargetARM64,
		Insts:  insts,
		Operands: []Operand{
			{Reg: 2, Bank: BankGPR, Flags: OperandUse},
			{Reg: 3, Bank: BankGPR, Flags: OperandUse},
			{Reg: 1, Bank: BankGPR, Flags: OperandUse},
		},
		VRegs:  []VRegData{{}, {Type: TypeI32, Bank: BankGPR}, {Type: TypeI32, Bank: BankGPR}, {Type: TypeI32, Bank: BankGPR}},
		Blocks: []Block{{InstCount: uint32(len(insts))}},
	}
	order := make([]uint32, 0, len(insts))
	for instruction := uint32(1); instruction < consumer; instruction++ {
		order = append(order, instruction)
	}
	order = append(order, 0, consumer)
	schedule := &Schedule{Order: order, BlockOf: make([]railssa.BlockID, len(insts))}
	selection := &SelectionPlan{Combinations: []Combination{{Producer: 0, Consumer: consumer, Kind: CombineCompareBranch}}}
	plan := &PostRAPlan{Rewrites: []Rewrite{{First: 0, Second: consumer, Kind: RewriteARM64CompareBranch}}, ScanLimit: PostRAScanLimit}
	if err := VerifyPostRAPlan(TargetARM64, f, selection, schedule, plan); err != nil {
		t.Fatal(err)
	}
	schedule.Order[len(schedule.Order)-1], schedule.Order[len(schedule.Order)-2] = schedule.Order[len(schedule.Order)-2], schedule.Order[len(schedule.Order)-1]
	if err := VerifyPostRAPlan(TargetARM64, f, selection, schedule, plan); err == nil {
		t.Fatal("accepted nonadjacent ARM64 compare/branch rewrite")
	}
}

func TestVerifyPostRAAllowsAdjacentARM64FloatCompareBranch(t *testing.T) {
	f := &Func{
		Target: TargetARM64,
		Insts: []Inst{
			{Op: wasm.InstrF64Gt, Result: 1, OperandStart: 0, OperandCount: 2},
			{Op: wasm.InstrBrIf, OperandStart: 2, OperandCount: 1},
		},
		Operands: []Operand{
			{Reg: 2, Bank: BankFPR, Flags: OperandUse},
			{Reg: 3, Bank: BankFPR, Flags: OperandUse},
			{Reg: 1, Bank: BankGPR, Flags: OperandUse},
		},
		VRegs: []VRegData{
			{},
			{Type: TypeI32, Bank: BankGPR},
			{Type: TypeF64, Bank: BankFPR},
			{Type: TypeF64, Bank: BankFPR},
		},
		Blocks: []Block{{InstCount: 2}},
	}
	schedule := &Schedule{Order: []uint32{0, 1}, BlockOf: make([]railssa.BlockID, 2)}
	selection := &SelectionPlan{Combinations: []Combination{{Producer: 0, Consumer: 1, Kind: CombineCompareBranch}}}
	plan := &PostRAPlan{Rewrites: []Rewrite{{First: 0, Second: 1, Kind: RewriteARM64CompareBranch}}, ScanLimit: PostRAScanLimit}
	if err := VerifyPostRAPlan(TargetARM64, f, selection, schedule, plan); err != nil {
		t.Fatal(err)
	}
	f.Insts[0].Op = wasm.InstrF64Add
	if err := VerifyPostRAPlan(TargetARM64, f, selection, schedule, plan); err == nil {
		t.Fatal("accepted non-comparison ARM64 flag producer")
	}
}

func TestVerifyPostRAAllowsARM64NZCVRenameAcrossConstant(t *testing.T) {
	f := &Func{
		Target: TargetARM64,
		Insts: []Inst{
			{Op: wasm.InstrI32LtS, Result: 1, OperandStart: 0, OperandCount: 2},
			{Op: wasm.InstrI32Const, Result: 4},
			{Op: wasm.InstrIf, OperandStart: 2, OperandCount: 1},
		},
		Operands: []Operand{
			{Reg: 2, Bank: BankGPR, Flags: OperandUse},
			{Reg: 3, Bank: BankGPR, Flags: OperandUse},
			{Reg: 1, Bank: BankGPR, Flags: OperandUse},
		},
		VRegs:  []VRegData{{}, {Type: TypeI32, Bank: BankGPR}, {Type: TypeI32, Bank: BankGPR}, {Type: TypeI32, Bank: BankGPR}, {Type: TypeI32, Bank: BankGPR}},
		Blocks: []Block{{InstCount: 3}},
	}
	schedule := &Schedule{Order: []uint32{0, 1, 2}, BlockOf: make([]railssa.BlockID, 3)}
	selection := &SelectionPlan{Combinations: []Combination{{Producer: 0, Consumer: 2, Kind: CombineCompareBranch}}}
	plan := &PostRAPlan{Rewrites: []Rewrite{{First: 0, Second: 2, Kind: RewritePhysicalRename}}, ScanLimit: PostRAScanLimit}
	if err := VerifyPostRAPlan(TargetARM64, f, selection, schedule, plan); err != nil {
		t.Fatal(err)
	}
	f.Insts[1] = Inst{Op: wasm.InstrF32Eq, Result: 4}
	if err := VerifyPostRAPlan(TargetARM64, f, selection, schedule, plan); err == nil {
		t.Fatal("accepted NZCV rename across a flag-clobbering float comparison")
	}
	f.Insts[1] = Inst{Op: wasm.InstrI32Const, Result: 4}
	if err := VerifyPostRAPlan(TargetAMD64, f, selection, schedule, plan); err != nil {
		t.Fatalf("AMD64 EFLAGS rename rejected: %v", err)
	}
}

func TestVerifyARM64ByteSwapChain(t *testing.T) {
	f := &Func{
		Target: TargetARM64,
		Insts: []Inst{
			{Op: wasm.InstrI32Const, Result: 2, Aux: 0x00ff00ff},
			{Op: wasm.InstrI32And, Result: 3, OperandStart: 0, OperandCount: 2},
			{Op: wasm.InstrI32Const, Result: 4, Aux: 8},
			{Op: wasm.InstrI32Rotr, Result: 5, OperandStart: 2, OperandCount: 2},
			{Op: wasm.InstrI32Const, Result: 6, Aux: 24},
			{Op: wasm.InstrI32Rotr, Result: 7, OperandStart: 4, OperandCount: 2},
			{Op: wasm.InstrI32And, Result: 8, OperandStart: 6, OperandCount: 2},
			{Op: wasm.InstrI32Or, Result: 9, OperandStart: 8, OperandCount: 2},
		},
		Operands: []Operand{
			{Reg: 1, Bank: BankGPR}, {Reg: 2, Bank: BankGPR},
			{Reg: 3, Bank: BankGPR}, {Reg: 4, Bank: BankGPR},
			{Reg: 1, Bank: BankGPR}, {Reg: 6, Bank: BankGPR},
			{Reg: 7, Bank: BankGPR}, {Reg: 2, Bank: BankGPR},
			{Reg: 5, Bank: BankGPR}, {Reg: 8, Bank: BankGPR},
		},
		VRegs: []VRegData{
			{}, {Type: TypeI32, Bank: BankGPR, Flags: VRegInitial},
			{Def: 3, Type: TypeI32, Bank: BankGPR}, {Def: 9, Type: TypeI32, Bank: BankGPR},
			{Def: 15, Type: TypeI32, Bank: BankGPR}, {Def: 21, Type: TypeI32, Bank: BankGPR},
			{Def: 27, Type: TypeI32, Bank: BankGPR}, {Def: 33, Type: TypeI32, Bank: BankGPR},
			{Def: 39, Type: TypeI32, Bank: BankGPR}, {Def: 45, Type: TypeI32, Bank: BankGPR},
		},
		Blocks: []Block{{InstCount: 8}},
	}
	schedule := &Schedule{Order: []uint32{0, 1, 2, 3, 4, 5, 6, 7}, BlockRanges: []MoveRange{{Count: 8}}, BlockOf: make([]railssa.BlockID, 8)}
	source, members, ok := VerifyARM64ByteSwapChain(f, schedule, 7)
	if !ok || source != 1 || members != [5]uint32{1, 3, 5, 6, 7} {
		t.Fatalf("byte swap = source %d, members %v, ok %t", source, members, ok)
	}
	f.Insts[4].Aux = 23
	if _, _, ok := VerifyARM64ByteSwapChain(f, schedule, 7); ok {
		t.Fatal("byte swap accepted rotate by 23")
	}
}

func TestPlanPostRAFindsARM64RepeatedAdd(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x20, 0x01, 0x6a,
		0x20, 0x01, 0x6a,
		0x20, 0x01, 0x6a,
		0x20, 0x01, 0x6a,
		0x0b,
	})
	f, selection, _, dag := buildScheduleTest(t, TargetARM64, m)
	schedule, err := BuildSchedule(f, selection, dag, ScheduleKindSourceStable, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := AllocateGreedyPForSchedule(f, schedule, DefaultGreedyConfig(TargetARM64), nil)
	if err != nil {
		t.Fatal(err)
	}
	exit, err := LateSSAExit(f, &allocation.Allocation, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanPostRA(TargetARM64, f, selection, schedule, allocation, exit, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, rewrite := range plan.Rewrites {
		if rewrite.Kind != RewriteARM64RepeatedAdd {
			continue
		}
		initial, invariant, count, ok := VerifyARM64RepeatedAddChain(f, schedule, rewrite.First, rewrite.Second)
		if !ok || initial == invariant || count != 4 {
			t.Fatalf("repeated add = initial %d invariant %d count %d ok %v", initial, invariant, count, ok)
		}
		return
	}
	t.Fatalf("rewrites = %#v", plan.Rewrites)
}

func TestVerifyPostRAARM64PreIndexRange(t *testing.T) {
	f := &Func{Insts: []Inst{{Op: wasm.InstrI32Load, Aux: 7}}, Blocks: []Block{{InstCount: 1}}}
	schedule := &Schedule{Order: []uint32{0}, BlockOf: []railssa.BlockID{0}}
	selection := &SelectionPlan{Selections: make([]Selection, 1)}
	plan := &PostRAPlan{Rewrites: []Rewrite{{First: 0, Second: ^uint32(0), Kind: RewriteARM64PrePostIndex}}, ScanLimit: PostRAScanLimit}
	if err := VerifyPostRAPlan(TargetARM64, f, selection, schedule, plan); err != nil {
		t.Fatal(err)
	}
	f.Insts[0].Aux = 256
	if err := VerifyPostRAPlan(TargetARM64, f, selection, schedule, plan); err == nil {
		t.Fatal("accepted ARM64 pre-index displacement 256")
	}
	if err := VerifyPostRAPlan(TargetAMD64, f, selection, schedule, plan); err == nil {
		t.Fatal("accepted ARM64 pre-index rewrite on AMD64")
	}
}

func TestVerifyPostRAARM64PostIndexChain(t *testing.T) {
	f := &Func{
		Insts: []Inst{
			{Op: wasm.InstrI32Load8U, Aux: 300, OperandStart: 0, OperandCount: 1},
			{Op: wasm.InstrI32Load16U, Aux: 301, OperandStart: 1, OperandCount: 1},
		},
		Operands: []Operand{{Reg: 1}, {Reg: 1}},
		VRegs:    make([]VRegData, 2),
		Blocks:   []Block{{InstCount: 2}},
	}
	schedule := &Schedule{Order: []uint32{0, 1}, BlockOf: []railssa.BlockID{0, 0}}
	selection := &SelectionPlan{Selections: make([]Selection, 2)}
	plan := &PostRAPlan{Rewrites: []Rewrite{{First: 0, Second: 1, Kind: RewriteARM64PrePostIndex}}, ScanLimit: PostRAScanLimit}
	if err := VerifyPostRAPlan(TargetARM64, f, selection, schedule, plan); err != nil {
		t.Fatal(err)
	}
	f.Insts[1].Aux = 556
	if err := VerifyPostRAPlan(TargetARM64, f, selection, schedule, plan); err == nil {
		t.Fatal("accepted ARM64 post-index delta 256")
	}
	f.Insts[1].Aux = 301
	f.Operands[1].Reg = 0
	if err := VerifyPostRAPlan(TargetARM64, f, selection, schedule, plan); err == nil {
		t.Fatal("accepted ARM64 post-index chain with a different Wasm base")
	}
	f.Operands[1].Reg = 1
	schedule.BlockOf[1] = 1
	if err := VerifyPostRAPlan(TargetARM64, f, selection, schedule, plan); err == nil {
		t.Fatal("accepted ARM64 post-index chain across a block boundary")
	}
}

func TestPlanPostRAAMD64SubLEARequiresRepresentableImmediate(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		want bool
	}{
		{name: "immediate", body: []byte{0x20, 0x00, 0x42, 0x07, 0x7d, 0x0b}, want: true},
		{name: "register", body: []byte{0x20, 0x00, 0x20, 0x01, 0x7d, 0x0b}},
		{name: "min-i32", body: []byte{0x20, 0x00, 0x42, 0x80, 0x80, 0x80, 0x80, 0x78, 0x7d, 0x0b}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := machineModule([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, tc.body)
			f, selection, _, dag := buildScheduleTest(t, TargetAMD64, m)
			schedule, err := BuildSchedule(f, selection, dag, ScheduleKindSourceStable, nil)
			if err != nil {
				t.Fatal(err)
			}
			allocation, err := AllocateGreedyPForSchedule(f, schedule, DefaultGreedyConfig(TargetAMD64), nil)
			if err != nil {
				t.Fatal(err)
			}
			exit, err := LateSSAExit(f, &allocation.Allocation, nil)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := PlanPostRA(TargetAMD64, f, selection, schedule, allocation, exit, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := false
			for _, rewrite := range plan.Rewrites {
				got = got || rewrite.Kind == RewriteAMD64LEA
			}
			if got != tc.want {
				t.Fatalf("LEA rewrite = %v, want %v; rewrites=%#v", got, tc.want, plan.Rewrites)
			}
		})
	}
}

func TestARM64PairRequiresExactAdjacentWidth(t *testing.T) {
	first := Inst{Op: wasm.InstrI64Load, Aux: 8}
	if !pairableMemory(first, Inst{Op: wasm.InstrI64Load, Aux: 16}) {
		t.Fatal("adjacent i64 loads did not pair")
	}
	if pairableMemory(first, Inst{Op: wasm.InstrI64Load, Aux: 12}) || pairableMemory(first, Inst{Op: wasm.InstrI32Load, Aux: 16}) {
		t.Fatal("non-adjacent or mismatched loads paired")
	}
	if !pairableMemory(Inst{Op: wasm.InstrF32Load, Aux: 8}, Inst{Op: wasm.InstrF32Load, Aux: 12}) {
		t.Fatal("adjacent f32 loads did not pair")
	}
	if pairableMemory(Inst{Op: wasm.InstrI32Store, Aux: 8}, Inst{Op: wasm.InstrI32Store, Aux: 12}) ||
		pairableMemory(Inst{Op: wasm.InstrI32Load16U, Aux: 8}, Inst{Op: wasm.InstrI32Load16U, Aux: 10}) {
		t.Fatal("ordered store or narrow accesses selected an architectural pair")
	}
}

func TestPlanPostRAFindsAMD64FullWidthMemoryFold(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x01, 0x20, 0x00, 0x28, 0x02, 0x00, 0x6a, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	f, selection, _, dag := buildScheduleTest(t, TargetAMD64, m)
	schedule, err := BuildSchedule(f, selection, dag, ScheduleKindSourceStable, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := AllocateGreedyPForSchedule(f, schedule, DefaultGreedyConfig(TargetAMD64), nil)
	if err != nil {
		t.Fatal(err)
	}
	exit, err := LateSSAExit(f, &allocation.Allocation, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanPostRA(TargetAMD64, f, selection, schedule, allocation, exit, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, rewrite := range plan.Rewrites {
		if rewrite.Kind == RewriteAMD64MemoryFold {
			return
		}
	}
	t.Fatalf("rewrites = %#v", plan.Rewrites)
}
