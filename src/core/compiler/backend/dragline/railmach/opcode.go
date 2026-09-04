package railmach

import "github.com/wago-org/wago/src/core/compiler/wasm"

// MOpcode is the machine instruction opcode namespace. Generic operations keep
// their validated Wasm opcode value; target selection replaces them in place
// with an opcode from the disjoint selected range. Keeping the same compact
// representation lets selection refine RailMach instead of creating a third
// instruction IR.
type MOpcode = wasm.InstrKind

const selectedOpcodeBase MOpcode = 0x8000

const (
	OpAMD64V128Move MOpcode = selectedOpcodeBase + iota
	OpAMD64V128Load
	OpAMD64V128Store
	OpAMD64V128And
	OpAMD64V128Or
	OpAMD64V128Xor
	opAMD64SelectedEnd
)

const (
	OpARM64V128Move MOpcode = 0x9000 + iota
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
