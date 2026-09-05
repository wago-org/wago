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
	OpAMD64I8x16Add
	OpAMD64I8x16AddSatS
	OpAMD64I8x16AddSatU
	OpAMD64I8x16Sub
	OpAMD64I8x16SubSatS
	OpAMD64I8x16SubSatU
	OpAMD64I16x8Add
	OpAMD64I16x8AddSatS
	OpAMD64I16x8AddSatU
	OpAMD64I16x8Sub
	OpAMD64I16x8SubSatS
	OpAMD64I16x8SubSatU
	OpAMD64I32x4Add
	OpAMD64I32x4Sub
	OpAMD64I64x2Add
	OpAMD64I64x2Sub
	OpAMD64I8x16Eq
	OpAMD64I8x16Ne
	OpAMD64I16x8Eq
	OpAMD64I16x8Ne
	OpAMD64I32x4Eq
	OpAMD64I32x4Ne
	OpAMD64I64x2Eq
	OpAMD64I64x2Ne
	OpAMD64I8x16LtS
	OpAMD64I8x16GtS
	OpAMD64I8x16LeS
	OpAMD64I8x16GeS
	OpAMD64I16x8LtS
	OpAMD64I16x8GtS
	OpAMD64I16x8LeS
	OpAMD64I16x8GeS
	OpAMD64I32x4LtS
	OpAMD64I32x4GtS
	OpAMD64I32x4LeS
	OpAMD64I32x4GeS
	OpAMD64I64x2LtS
	OpAMD64I64x2GtS
	OpAMD64I64x2LeS
	OpAMD64I64x2GeS
	OpAMD64I8x16LtU
	OpAMD64I8x16GtU
	OpAMD64I8x16LeU
	OpAMD64I8x16GeU
	OpAMD64I16x8LtU
	OpAMD64I16x8GtU
	OpAMD64I16x8LeU
	OpAMD64I16x8GeU
	OpAMD64I32x4LtU
	OpAMD64I32x4GtU
	OpAMD64I32x4LeU
	OpAMD64I32x4GeU
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
	OpARM64I8x16Add
	OpARM64I8x16AddSatS
	OpARM64I8x16AddSatU
	OpARM64I8x16Sub
	OpARM64I8x16SubSatS
	OpARM64I8x16SubSatU
	OpARM64I16x8Add
	OpARM64I16x8AddSatS
	OpARM64I16x8AddSatU
	OpARM64I16x8Sub
	OpARM64I16x8SubSatS
	OpARM64I16x8SubSatU
	OpARM64I32x4Add
	OpARM64I32x4Sub
	OpARM64I64x2Add
	OpARM64I64x2Sub
	OpARM64I8x16Eq
	OpARM64I8x16Ne
	OpARM64I16x8Eq
	OpARM64I16x8Ne
	OpARM64I32x4Eq
	OpARM64I32x4Ne
	OpARM64I64x2Eq
	OpARM64I64x2Ne
	OpARM64I8x16LtS
	OpARM64I8x16GtS
	OpARM64I8x16LeS
	OpARM64I8x16GeS
	OpARM64I16x8LtS
	OpARM64I16x8GtS
	OpARM64I16x8LeS
	OpARM64I16x8GeS
	OpARM64I32x4LtS
	OpARM64I32x4GtS
	OpARM64I32x4LeS
	OpARM64I32x4GeS
	OpARM64I64x2LtS
	OpARM64I64x2GtS
	OpARM64I64x2LeS
	OpARM64I64x2GeS
	OpARM64I8x16LtU
	OpARM64I8x16GtU
	OpARM64I8x16LeU
	OpARM64I8x16GeU
	OpARM64I16x8LtU
	OpARM64I16x8GtU
	OpARM64I16x8LeU
	OpARM64I16x8GeU
	OpARM64I32x4LtU
	OpARM64I32x4GtU
	OpARM64I32x4LeU
	OpARM64I32x4GeU
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
		OpAMD64I8x16Add, OpAMD64I8x16AddSatS, OpAMD64I8x16AddSatU, OpAMD64I8x16Sub, OpAMD64I8x16SubSatS, OpAMD64I8x16SubSatU,
		OpAMD64I16x8Add, OpAMD64I16x8AddSatS, OpAMD64I16x8AddSatU, OpAMD64I16x8Sub, OpAMD64I16x8SubSatS, OpAMD64I16x8SubSatU,
		OpAMD64I32x4Add, OpAMD64I32x4Sub, OpAMD64I64x2Add, OpAMD64I64x2Sub,
		OpAMD64I8x16Eq, OpAMD64I8x16Ne, OpAMD64I16x8Eq, OpAMD64I16x8Ne,
		OpAMD64I32x4Eq, OpAMD64I32x4Ne, OpAMD64I64x2Eq, OpAMD64I64x2Ne,
		OpAMD64I8x16LtS, OpAMD64I8x16GtS, OpAMD64I8x16LeS, OpAMD64I8x16GeS,
		OpAMD64I16x8LtS, OpAMD64I16x8GtS, OpAMD64I16x8LeS, OpAMD64I16x8GeS,
		OpAMD64I32x4LtS, OpAMD64I32x4GtS, OpAMD64I32x4LeS, OpAMD64I32x4GeS,
		OpAMD64I64x2LtS, OpAMD64I64x2GtS, OpAMD64I64x2LeS, OpAMD64I64x2GeS,
		OpAMD64I8x16LtU, OpAMD64I8x16GtU, OpAMD64I8x16LeU, OpAMD64I8x16GeU,
		OpAMD64I16x8LtU, OpAMD64I16x8GtU, OpAMD64I16x8LeU, OpAMD64I16x8GeU,
		OpAMD64I32x4LtU, OpAMD64I32x4GtU, OpAMD64I32x4LeU, OpAMD64I32x4GeU,
		OpARM64V128Move, OpARM64V128Const, OpARM64V128Load, OpARM64V128Store, OpARM64V128And, OpARM64V128Or, OpARM64V128Xor,
		OpARM64I8x16Add, OpARM64I8x16AddSatS, OpARM64I8x16AddSatU, OpARM64I8x16Sub, OpARM64I8x16SubSatS, OpARM64I8x16SubSatU,
		OpARM64I16x8Add, OpARM64I16x8AddSatS, OpARM64I16x8AddSatU, OpARM64I16x8Sub, OpARM64I16x8SubSatS, OpARM64I16x8SubSatU,
		OpARM64I32x4Add, OpARM64I32x4Sub, OpARM64I64x2Add, OpARM64I64x2Sub,
		OpARM64I8x16Eq, OpARM64I8x16Ne, OpARM64I16x8Eq, OpARM64I16x8Ne,
		OpARM64I32x4Eq, OpARM64I32x4Ne, OpARM64I64x2Eq, OpARM64I64x2Ne,
		OpARM64I8x16LtS, OpARM64I8x16GtS, OpARM64I8x16LeS, OpARM64I8x16GeS,
		OpARM64I16x8LtS, OpARM64I16x8GtS, OpARM64I16x8LeS, OpARM64I16x8GeS,
		OpARM64I32x4LtS, OpARM64I32x4GtS, OpARM64I32x4LeS, OpARM64I32x4GeS,
		OpARM64I64x2LtS, OpARM64I64x2GtS, OpARM64I64x2LeS, OpARM64I64x2GeS,
		OpARM64I8x16LtU, OpARM64I8x16GtU, OpARM64I8x16LeU, OpARM64I8x16GeU,
		OpARM64I16x8LtU, OpARM64I16x8GtU, OpARM64I16x8LeU, OpARM64I16x8GeU,
		OpARM64I32x4LtU, OpARM64I32x4GtU, OpARM64I32x4LeU, OpARM64I32x4GeU:
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
		case wasm.InstrI8x16Add:
			amd64, arm64 = OpAMD64I8x16Add, OpARM64I8x16Add
		case wasm.InstrI8x16AddSatS:
			amd64, arm64 = OpAMD64I8x16AddSatS, OpARM64I8x16AddSatS
		case wasm.InstrI8x16AddSatU:
			amd64, arm64 = OpAMD64I8x16AddSatU, OpARM64I8x16AddSatU
		case wasm.InstrI8x16Sub:
			amd64, arm64 = OpAMD64I8x16Sub, OpARM64I8x16Sub
		case wasm.InstrI8x16SubSatS:
			amd64, arm64 = OpAMD64I8x16SubSatS, OpARM64I8x16SubSatS
		case wasm.InstrI8x16SubSatU:
			amd64, arm64 = OpAMD64I8x16SubSatU, OpARM64I8x16SubSatU
		case wasm.InstrI16x8Add:
			amd64, arm64 = OpAMD64I16x8Add, OpARM64I16x8Add
		case wasm.InstrI16x8AddSatS:
			amd64, arm64 = OpAMD64I16x8AddSatS, OpARM64I16x8AddSatS
		case wasm.InstrI16x8AddSatU:
			amd64, arm64 = OpAMD64I16x8AddSatU, OpARM64I16x8AddSatU
		case wasm.InstrI16x8Sub:
			amd64, arm64 = OpAMD64I16x8Sub, OpARM64I16x8Sub
		case wasm.InstrI16x8SubSatS:
			amd64, arm64 = OpAMD64I16x8SubSatS, OpARM64I16x8SubSatS
		case wasm.InstrI16x8SubSatU:
			amd64, arm64 = OpAMD64I16x8SubSatU, OpARM64I16x8SubSatU
		case wasm.InstrI32x4Add:
			amd64, arm64 = OpAMD64I32x4Add, OpARM64I32x4Add
		case wasm.InstrI32x4Sub:
			amd64, arm64 = OpAMD64I32x4Sub, OpARM64I32x4Sub
		case wasm.InstrI64x2Add:
			amd64, arm64 = OpAMD64I64x2Add, OpARM64I64x2Add
		case wasm.InstrI64x2Sub:
			amd64, arm64 = OpAMD64I64x2Sub, OpARM64I64x2Sub
		case wasm.InstrI8x16Eq:
			amd64, arm64 = OpAMD64I8x16Eq, OpARM64I8x16Eq
		case wasm.InstrI8x16Ne:
			amd64, arm64 = OpAMD64I8x16Ne, OpARM64I8x16Ne
		case wasm.InstrI16x8Eq:
			amd64, arm64 = OpAMD64I16x8Eq, OpARM64I16x8Eq
		case wasm.InstrI16x8Ne:
			amd64, arm64 = OpAMD64I16x8Ne, OpARM64I16x8Ne
		case wasm.InstrI32x4Eq:
			amd64, arm64 = OpAMD64I32x4Eq, OpARM64I32x4Eq
		case wasm.InstrI32x4Ne:
			amd64, arm64 = OpAMD64I32x4Ne, OpARM64I32x4Ne
		case wasm.InstrI64x2Eq:
			amd64, arm64 = OpAMD64I64x2Eq, OpARM64I64x2Eq
		case wasm.InstrI64x2Ne:
			amd64, arm64 = OpAMD64I64x2Ne, OpARM64I64x2Ne
		case wasm.InstrI8x16LtS:
			amd64, arm64 = OpAMD64I8x16LtS, OpARM64I8x16LtS
		case wasm.InstrI8x16GtS:
			amd64, arm64 = OpAMD64I8x16GtS, OpARM64I8x16GtS
		case wasm.InstrI8x16LeS:
			amd64, arm64 = OpAMD64I8x16LeS, OpARM64I8x16LeS
		case wasm.InstrI8x16GeS:
			amd64, arm64 = OpAMD64I8x16GeS, OpARM64I8x16GeS
		case wasm.InstrI16x8LtS:
			amd64, arm64 = OpAMD64I16x8LtS, OpARM64I16x8LtS
		case wasm.InstrI16x8GtS:
			amd64, arm64 = OpAMD64I16x8GtS, OpARM64I16x8GtS
		case wasm.InstrI16x8LeS:
			amd64, arm64 = OpAMD64I16x8LeS, OpARM64I16x8LeS
		case wasm.InstrI16x8GeS:
			amd64, arm64 = OpAMD64I16x8GeS, OpARM64I16x8GeS
		case wasm.InstrI32x4LtS:
			amd64, arm64 = OpAMD64I32x4LtS, OpARM64I32x4LtS
		case wasm.InstrI32x4GtS:
			amd64, arm64 = OpAMD64I32x4GtS, OpARM64I32x4GtS
		case wasm.InstrI32x4LeS:
			amd64, arm64 = OpAMD64I32x4LeS, OpARM64I32x4LeS
		case wasm.InstrI32x4GeS:
			amd64, arm64 = OpAMD64I32x4GeS, OpARM64I32x4GeS
		case wasm.InstrI64x2LtS:
			amd64, arm64 = OpAMD64I64x2LtS, OpARM64I64x2LtS
		case wasm.InstrI64x2GtS:
			amd64, arm64 = OpAMD64I64x2GtS, OpARM64I64x2GtS
		case wasm.InstrI64x2LeS:
			amd64, arm64 = OpAMD64I64x2LeS, OpARM64I64x2LeS
		case wasm.InstrI64x2GeS:
			amd64, arm64 = OpAMD64I64x2GeS, OpARM64I64x2GeS
		case wasm.InstrI8x16LtU:
			amd64, arm64 = OpAMD64I8x16LtU, OpARM64I8x16LtU
		case wasm.InstrI8x16GtU:
			amd64, arm64 = OpAMD64I8x16GtU, OpARM64I8x16GtU
		case wasm.InstrI8x16LeU:
			amd64, arm64 = OpAMD64I8x16LeU, OpARM64I8x16LeU
		case wasm.InstrI8x16GeU:
			amd64, arm64 = OpAMD64I8x16GeU, OpARM64I8x16GeU
		case wasm.InstrI16x8LtU:
			amd64, arm64 = OpAMD64I16x8LtU, OpARM64I16x8LtU
		case wasm.InstrI16x8GtU:
			amd64, arm64 = OpAMD64I16x8GtU, OpARM64I16x8GtU
		case wasm.InstrI16x8LeU:
			amd64, arm64 = OpAMD64I16x8LeU, OpARM64I16x8LeU
		case wasm.InstrI16x8GeU:
			amd64, arm64 = OpAMD64I16x8GeU, OpARM64I16x8GeU
		case wasm.InstrI32x4LtU:
			amd64, arm64 = OpAMD64I32x4LtU, OpARM64I32x4LtU
		case wasm.InstrI32x4GtU:
			amd64, arm64 = OpAMD64I32x4GtU, OpARM64I32x4GtU
		case wasm.InstrI32x4LeU:
			amd64, arm64 = OpAMD64I32x4LeU, OpARM64I32x4LeU
		case wasm.InstrI32x4GeU:
			amd64, arm64 = OpAMD64I32x4GeU, OpARM64I32x4GeU
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
