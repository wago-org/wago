package railmach

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestScheduleSelectedControlLast(t *testing.T) {
	// The unused comparison must still be placed before the branch. Otherwise
	// its emitted CMP overwrites the flags of a fused comparison and branch.
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00, 0x41, 0x01, 0x47, 0x1a,
		0x20, 0x00, 0x41, 0x08, 0x4f,
		0x04, 0x7f, 0x41, 0x01, 0x05, 0x41, 0x02, 0x0b, 0x0b,
	})
	for _, target := range []Target{TargetAMD64, TargetARM64} {
		for _, kind := range []ScheduleKind{ScheduleKindSourceStable, ScheduleKindLatencyFusion, ScheduleKindPressure} {
			t.Run(fmt.Sprintf("%s/%d", target, kind), func(t *testing.T) {
				f, selection, _, dag := buildScheduleTest(t, target, m)
				if _, err := SelectTargetOpcodes(f); err != nil {
					t.Fatal(err)
				}
				// The constant feeding the branch comparison is a valid pressure
				// sink before fusion reserves comparison/branch adjacency.
				pressure := &railssa.PressurePlan{Sinks: []railssa.SinkMove{{Instruction: 2, Before: 3, Block: 0}}}
				schedule, err := BuildScheduleWithPressure(f, selection, dag, kind, pressure, nil)
				if err != nil {
					t.Fatal(err)
				}
				for _, block := range schedule.BlockRanges {
					for offset, id := range schedule.Order[block.Start : block.Start+block.Count] {
						if SemanticOpcode(f.Insts[id].Op) == wasm.InstrIf && uint32(offset)+1 != block.Count {
							t.Fatalf("branch %d precedes other instructions: %v", id, schedule.Order)
						}
					}
				}
			})
		}
	}
}

func TestVerifyScheduleRejectsInstructionAfterControl(t *testing.T) {
	for _, op := range []MOpcode{wasm.InstrBrIf, OpAMD64BrIf} {
		f := &Func{Insts: []Inst{{Op: wasm.InstrI32Const}, {Op: op}}, Blocks: []Block{{InstCount: 2}}}
		dag := &DependencyDAG{Offsets: []uint32{0, 0, 0}}
		schedule := &Schedule{Order: []uint32{1, 0}, BlockRanges: []MoveRange{{Count: 2}}}
		if err := VerifySchedule(f, dag, schedule); err == nil {
			t.Fatalf("accepted instruction after control opcode %d", op)
		}
	}
}
