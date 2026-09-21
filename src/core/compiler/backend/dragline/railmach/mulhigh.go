package railmach

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// arm64MulHighMatch is intentionally fixed-size: the portable 64x64 unsigned
// multiply-high expansion has a bounded DAG, so selecting it must not add a
// function-sized allocation to the compiler hot path.
type arm64MulHighMatch struct {
	x, y         VReg
	instructions [24]uint32
	count        uint8
}

func (m *arm64MulHighMatch) containsInstruction(id uint32) bool {
	for _, member := range m.instructions[:m.count] {
		if member == id {
			return true
		}
	}
	return false
}

func (m *arm64MulHighMatch) addInstruction(id uint32) bool {
	if m.containsInstruction(id) {
		return true
	}
	if int(m.count) == len(m.instructions) {
		return false
	}
	m.instructions[m.count] = id
	m.count++
	return true
}

// SelectARM64MulHighIdioms contracts the canonical portable unsigned 64x64
// multiply-high DAG to ARM64 UMULH. This is an ordinary machine-SSA identity:
// it is independent of module, function, export, constants surrounding the
// operands, and corpus identity.
//
// Every non-root member must be private to the matched DAG. That rule makes
// elision safe even when a producer is shared several times inside the idiom,
// while an escaping intermediate causes the entire match to be rejected.
func SelectARM64MulHighIdioms(f *Func) (uint32, error) {
	if err := Verify(f); err != nil {
		return 0, err
	}
	if f.Target != TargetARM64 {
		return 0, nil
	}
	var selected uint32
	for root := range f.Insts {
		match, ok := matchARM64MulHighU(f, uint32(root))
		if !ok || !arm64MulHighMembersPrivate(f, uint32(root), &match) {
			continue
		}
		instruction := &f.Insts[root]
		operands := f.InstructionOperands(uint32(root))
		if len(operands) != 2 {
			return 0, fmt.Errorf("railmach: multiply-high root %d lost binary operands", root)
		}
		operands[0] = Operand{Reg: match.x, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse}
		operands[1] = Operand{Reg: match.y, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse}
		instruction.Op = OpARM64I64MulHighU
		for _, member := range match.instructions[:match.count] {
			if member == uint32(root) {
				continue
			}
			memberInstruction := &f.Insts[member]
			result := memberInstruction.Result
			if result != 0 {
				f.VRegs[result].Flags |= VRegElided
			}
			// The member is now semantically dead. Detach its uses as well as its
			// definition so liveness and ABI analysis do not reserve registers for
			// an expansion that the finalizer will never emit.
			memberInstruction.OperandCount = 0
		}
		selected++
	}
	if err := Verify(f); err != nil {
		return 0, err
	}
	return selected, nil
}

func matchARM64MulHighU(f *Func, root uint32) (arm64MulHighMatch, bool) {
	rootOperands, ok := arm64MulHighBinary(f, root, wasm.InstrI64Add)
	if !ok || f.Insts[root].Result == 0 || f.VRegs[f.Insts[root].Result].Type != TypeI64 {
		return arm64MulHighMatch{}, false
	}
	for rootOrder := 0; rootOrder < 2; rootOrder++ {
		upper, lowerHigh := rootOperands[rootOrder], rootOperands[1-rootOrder]
		lower, _, ok := arm64MulHighShift32(f, lowerHigh)
		if !ok {
			continue
		}
		upperOperands, ok := arm64MulHighValueBinary(f, upper, wasm.InstrI64Add)
		if !ok {
			continue
		}
		lowerOperands, ok := arm64MulHighValueBinary(f, lower, wasm.InstrI64Add)
		if !ok {
			continue
		}
		for upperOrder := 0; upperOrder < 2; upperOrder++ {
			upperProduct, carryHigh := upperOperands[upperOrder], upperOperands[1-upperOrder]
			carry, _, ok := arm64MulHighShift32(f, carryHigh)
			if !ok {
				continue
			}
			for lowerOrder := 0; lowerOrder < 2; lowerOrder++ {
				crossProduct, carryLow := lowerOperands[lowerOrder], lowerOperands[1-lowerOrder]
				if base, _, ok := arm64MulHighMask32(f, carryLow); !ok || base != carry {
					continue
				}
				carryOperands, ok := arm64MulHighValueBinary(f, carry, wasm.InstrI64Add)
				if !ok {
					continue
				}
				for carryOrder := 0; carryOrder < 2; carryOrder++ {
					firstProduct, lowProductHigh := carryOperands[carryOrder], carryOperands[1-carryOrder]
					lowProduct, _, ok := arm64MulHighShift32(f, lowProductHigh)
					if !ok {
						continue
					}
					lowOperands, ok := arm64MulHighValueBinary(f, lowProduct, wasm.InstrI64Mul)
					if !ok {
						continue
					}
					for lowOrder := 0; lowOrder < 2; lowOrder++ {
						xLow, yLow := lowOperands[lowOrder], lowOperands[1-lowOrder]
						x, _, xOK := arm64MulHighMask32(f, xLow)
						y, _, yOK := arm64MulHighMask32(f, yLow)
						if !xOK || !yOK {
							continue
						}
						xHigh, xShift, xOK := arm64MulHighFindShiftOperand(f, firstProduct, yLow, x)
						yHigh, yShift, yOK := arm64MulHighFindShiftOperand(f, upperProduct, xHigh, y)
						if !xOK || !yOK || !arm64MulHighValueIsProduct(f, crossProduct, xLow, yHigh) {
							continue
						}
						match := arm64MulHighMatch{x: x, y: y}
						if !arm64MulHighCollect(f, f.Insts[root].Result, x, y, &match) ||
							!match.containsInstruction(xShift) || !match.containsInstruction(yShift) {
							continue
						}
						return match, true
					}
				}
			}
		}
	}
	return arm64MulHighMatch{}, false
}

func arm64MulHighDefinition(f *Func, value VReg) (uint32, Inst, []Operand, bool) {
	if value == 0 || int(value) >= len(f.VRegs) {
		return 0, Inst{}, nil, false
	}
	data := f.VRegs[value]
	if data.Flags&(VRegInitial|VRegBlockParam) != 0 {
		return 0, Inst{}, nil, false
	}
	id := data.Def / 6
	if int(id) >= len(f.Insts) {
		return 0, Inst{}, nil, false
	}
	instruction := f.Insts[id]
	if instruction.Result != value || instruction.ResultCount() != 1 {
		return 0, Inst{}, nil, false
	}
	return id, instruction, f.InstructionOperands(id), true
}

func arm64MulHighBinary(f *Func, instruction uint32, op MOpcode) ([2]VReg, bool) {
	if int(instruction) >= len(f.Insts) || SemanticOpcode(f.Insts[instruction].Op) != op {
		return [2]VReg{}, false
	}
	operands := f.InstructionOperands(instruction)
	if len(operands) != 2 {
		return [2]VReg{}, false
	}
	return [2]VReg{operands[0].Reg, operands[1].Reg}, true
}

func arm64MulHighValueBinary(f *Func, value VReg, op MOpcode) ([2]VReg, bool) {
	id, _, _, ok := arm64MulHighDefinition(f, value)
	if !ok {
		return [2]VReg{}, false
	}
	return arm64MulHighBinary(f, id, op)
}

func arm64MulHighConstant(f *Func, value VReg, want uint64) bool {
	_, instruction, operands, ok := arm64MulHighDefinition(f, value)
	return ok && instruction.Op == wasm.InstrI64Const && instruction.Aux == want && len(operands) == 0
}

func arm64MulHighShift32(f *Func, value VReg) (base VReg, instruction uint32, ok bool) {
	id, current, operands, ok := arm64MulHighDefinition(f, value)
	if !ok || current.Op != wasm.InstrI64ShrU || len(operands) != 2 || !arm64MulHighConstant(f, operands[1].Reg, 32) {
		return 0, 0, false
	}
	return operands[0].Reg, id, true
}

func arm64MulHighMask32(f *Func, value VReg) (base VReg, instruction uint32, ok bool) {
	id, current, operands, ok := arm64MulHighDefinition(f, value)
	if !ok || current.Op != wasm.InstrI64And || len(operands) != 2 {
		return 0, 0, false
	}
	if arm64MulHighConstant(f, operands[1].Reg, 0xffffffff) {
		return operands[0].Reg, id, true
	}
	if arm64MulHighConstant(f, operands[0].Reg, 0xffffffff) {
		return operands[1].Reg, id, true
	}
	return 0, 0, false
}

func arm64MulHighValueIsProduct(f *Func, value, a, b VReg) bool {
	operands, ok := arm64MulHighValueBinary(f, value, wasm.InstrI64Mul)
	return ok && (operands[0] == a && operands[1] == b || operands[0] == b && operands[1] == a)
}

func arm64MulHighFindShiftOperand(f *Func, product, other, source VReg) (shifted VReg, instruction uint32, ok bool) {
	operands, ok := arm64MulHighValueBinary(f, product, wasm.InstrI64Mul)
	if !ok {
		return 0, 0, false
	}
	for order := 0; order < 2; order++ {
		if operands[1-order] != other {
			continue
		}
		base, id, shiftedOK := arm64MulHighShift32(f, operands[order])
		if shiftedOK && base == source {
			return operands[order], id, true
		}
	}
	return 0, 0, false
}

func arm64MulHighCollect(f *Func, value, x, y VReg, match *arm64MulHighMatch) bool {
	if value == x || value == y {
		return true
	}
	id, _, operands, ok := arm64MulHighDefinition(f, value)
	if !ok {
		return false
	}
	if match.containsInstruction(id) {
		return true
	}
	if !match.addInstruction(id) {
		return false
	}
	for _, operand := range operands {
		if !arm64MulHighCollect(f, operand.Reg, x, y, match) {
			return false
		}
	}
	return true
}

func arm64MulHighMembersPrivate(f *Func, root uint32, match *arm64MulHighMatch) bool {
	for consumer := range f.Insts {
		for _, operand := range f.InstructionOperands(uint32(consumer)) {
			id, _, _, defined := arm64MulHighDefinition(f, operand.Reg)
			if defined && id != root && match.containsInstruction(id) && !match.containsInstruction(uint32(consumer)) {
				return false
			}
		}
	}
	for _, transfer := range f.Transfers {
		if id, _, _, ok := arm64MulHighDefinition(f, transfer.Src); ok && id != root && match.containsInstruction(id) {
			return false
		}
	}
	for _, result := range f.Results {
		if id, _, _, ok := arm64MulHighDefinition(f, result); ok && id != root && match.containsInstruction(id) {
			return false
		}
	}
	return true
}
