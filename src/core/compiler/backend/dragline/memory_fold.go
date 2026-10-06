package dragline

import (
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// planAMD64LateFloatMemoryFolds folds one-use scalar FP loads into nearby
// arithmetic. Only private memory reads can cross each other: with no intervening
// write or different trap, either failure is the same memory-bounds trap. Stores,
// calls, control flow, growth, atomics, and other trap classes remain barriers.
func planAMD64LateFloatMemoryFolds(stack *railssa.StackFunc, machine *railmach.Func, schedule *railmach.Schedule, allocation *railmach.GreedyAllocation, skip *nativeBitSet, from *nativeInstructionRelation, forwarded nativeInstructionRelation, addressRemat nativeBitSet, uses []uint32) {
	if machine.Target != railmach.TargetAMD64 || stack == nil || stack.Module == nil {
		return
	}
	memory, ok := stack.Module.MemoryType(0)
	if !ok || memory.Shared || memory.Limits.Addr64 {
		return
	}
	countNativeMachineUses(machine, uses)
	positions := allocation.InstructionPositions
	for loadID, load := range machine.Insts {
		op := railmach.SemanticOpcode(load.Op)
		if op != wasm.InstrF32Load && op != wasm.InstrF64Load || load.Result == 0 || uses[load.Result] != 1 || skip.has(uint32(loadID)) || forwarded.has(uint32(loadID)) {
			continue
		}
		access, ok := machine.MemoryAccessAt(uint32(loadID))
		if !ok || access.MemoryIndex != 0 || addressRemat.has(uint32(access.AddressValue)) {
			continue
		}
		start := positions[loadID]
		end := min(start+9, uint32(len(schedule.Order)))
		for pos := start + 1; pos < end; pos++ {
			id := schedule.Order[pos]
			in := machine.Insts[id]
			if schedule.BlockOf[id] != schedule.BlockOf[loadID] {
				break
			}
			args := machine.InstructionOperands(id)
			kind := railmach.SemanticOpcode(in.Op)
			isFP := op == wasm.InstrF64Load && (kind == wasm.InstrF64Add || kind == wasm.InstrF64Mul || kind == wasm.InstrF64Sub || kind == wasm.InstrF64Div) || op == wasm.InstrF32Load && (kind == wasm.InstrF32Add || kind == wasm.InstrF32Mul || kind == wasm.InstrF32Sub || kind == wasm.InstrF32Div)
			commute := kind == wasm.InstrF32Add || kind == wasm.InstrF32Mul || kind == wasm.InstrF64Add || kind == wasm.InstrF64Mul
			if isFP && len(args) == 2 && (args[1].Reg == load.Result || commute && args[0].Reg == load.Result) {
				if !from.has(id) && !skip.has(id) && railmach.AMD64FoldedAddressSurvivesWindow(machine, schedule, allocation, uint32(loadID), id) {
					if len(from.narrow)+len(from.wide) == 0 {
						from.prepare(len(machine.Insts), true)
					}
					if !skip.prepared(len(machine.Insts)) {
						skip.prepare(len(machine.Insts), true)
					}
					from.set(id, uint32(loadID))
					skip.set(uint32(loadID), true)
				}
				break
			}
			if nativeControlInstruction(in.Op) {
				break
			}
			effect := railssa.InstructionEffects(stack, in.Source)
			if effect.Reads == 0 && effect.Writes == 0 && effect.Flags == 0 && effect.Traps == 0 && effect.Obligations == 0 {
				continue
			}
			// Two private-memory reads have no intervening observable write. Either
			// out-of-bounds read still terminates with the same memory-bounds trap.
			if kind >= wasm.InstrI32Load && kind <= wasm.InstrI64Load32U && effect.Reads == railssa.HeapLinearMemory && effect.Writes == 0 && effect.Flags == railssa.EffectMayTrap && effect.Traps == railssa.TrapMemoryBounds {
				a, ok := machine.MemoryAccessAt(id)
				if ok && a.MemoryIndex == 0 {
					continue
				}
			}
			break
		}
	}
}
