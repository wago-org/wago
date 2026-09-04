package railmach

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// MOpcode is the machine instruction opcode namespace. Generic operations keep
// their validated Wasm opcode value; target selection replaces them in place
// with an opcode from the disjoint selected range. Keeping the same compact
// representation lets selection refine RailMach instead of creating a third
// instruction IR.
type MOpcode = wasm.InstrKind

const selectedOpcodeBase MOpcode = 0x8000

const (
	OpAMD64V128Move MOpcode = selectedOpcodeBase + iota
	OpAMD64V128Const
	OpAMD64V128Load
	OpAMD64V128Store
	OpAMD64V128And
	OpAMD64V128Or
	OpAMD64V128Xor
	opAMD64SelectedEnd
)

const (
	OpARM64V128Move MOpcode = 0x9000 + iota
	OpARM64V128Const
	OpARM64V128Load
	OpARM64V128Store
	OpARM64V128And
	OpARM64V128Or
	OpARM64V128Xor
	opARM64SelectedEnd
)

func IsSelectedOpcode(op MOpcode) bool { return op >= selectedOpcodeBase }

// SelectedOpcodeTarget reports the only target on which a selected opcode is
// legal. Generic operations return TargetInvalid.
func SelectedOpcodeTarget(op MOpcode) Target {
	switch {
	case op >= OpAMD64V128Move && op < opAMD64SelectedEnd:
		return TargetAMD64
	case op >= OpARM64V128Move && op < opARM64SelectedEnd:
		return TargetARM64
	default:
		return TargetInvalid
	}
}

func selectedMemoryWidth(op MOpcode) uint8 {
	switch op {
	case OpAMD64V128Load, OpAMD64V128Store, OpARM64V128Load, OpARM64V128Store:
		return 16
	default:
		return 0
	}
}

func isSelectedSIMDOpcode(op MOpcode) bool {
	switch op {
	case OpAMD64V128Move, OpAMD64V128Const, OpAMD64V128Load, OpAMD64V128Store, OpAMD64V128And, OpAMD64V128Or, OpAMD64V128Xor,
		OpARM64V128Move, OpARM64V128Const, OpARM64V128Load, OpARM64V128Store, OpARM64V128And, OpARM64V128Or, OpARM64V128Xor:
		return true
	default:
		return false
	}
}

// SelectTargetOpcodes refines the first admitted SIMD family in place after
// target-independent scheduling and allocation have completed. The finalizer
// therefore receives an explicit target operation rather than re-selecting a
// Wasm opcode. Later families can move this boundary earlier once their
// scheduling and constraint descriptions consume selected operations.
func SelectTargetOpcodes(f *Func) (int, error) {
	if err := Verify(f); err != nil {
		return 0, err
	}
	selected := 0
	for index := range f.Insts {
		instruction := &f.Insts[index]
		var amd64, arm64 MOpcode
		switch instruction.Op {
		case wasm.InstrV128Const:
			amd64, arm64 = OpAMD64V128Const, OpARM64V128Const
		case wasm.InstrV128Load:
			amd64, arm64 = OpAMD64V128Load, OpARM64V128Load
		case wasm.InstrV128Store:
			amd64, arm64 = OpAMD64V128Store, OpARM64V128Store
		case wasm.InstrV128And:
			amd64, arm64 = OpAMD64V128And, OpARM64V128And
		case wasm.InstrV128Or:
			amd64, arm64 = OpAMD64V128Or, OpARM64V128Or
		case wasm.InstrV128Xor:
			amd64, arm64 = OpAMD64V128Xor, OpARM64V128Xor
		default:
			continue
		}
		if f.Target == TargetAMD64 {
			instruction.Op = amd64
		} else if f.Target == TargetARM64 {
			instruction.Op = arm64
		} else {
			return 0, fmt.Errorf("railmach: cannot select opcodes for target %s", f.Target)
		}
		selected++
	}
	if err := Verify(f); err != nil {
		return 0, err
	}
	return selected, nil
}
