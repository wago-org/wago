package railspec

import "github.com/wago-org/wago/src/core/compiler/wasm"

func SelectRule(target TargetMask, kind wasm.InstrKind, rhsKnown bool, rhs uint64, feedsBranch bool) RuleID {
	if feedsBranch && comparison(kind) {
		return RuleCompareBranchFlags
	}
	if kind >= wasm.InstrI32Load && kind <= wasm.InstrI64Store32 {
		return RuleFoldedMemoryAddress
	}
	if target == TargetAMD64 {
		if shift(kind) {
			return RuleAMD64ShiftCL
		}
		if divide(kind) {
			return RuleAMD64DivFixed
		}
		if rhsKnown && rhs <= 0x7fffffff && integerALU(kind) {
			return RuleAMD64Imm32
		}
	}
	if target == TargetARM64 && rhsKnown && rhs <= 4095 && (kind == wasm.InstrI32Add || kind == wasm.InstrI32Sub || kind == wasm.InstrI64Add || kind == wasm.InstrI64Sub) {
		return RuleARM64Imm12
	}
	return RuleGenericRegister
}

func integerALU(kind wasm.InstrKind) bool {
	return kind >= wasm.InstrI32Add && kind <= wasm.InstrI32Rotr || kind >= wasm.InstrI64Add && kind <= wasm.InstrI64Rotr
}

func shift(kind wasm.InstrKind) bool {
	return kind >= wasm.InstrI32Shl && kind <= wasm.InstrI32Rotr || kind >= wasm.InstrI64Shl && kind <= wasm.InstrI64Rotr
}

func divide(kind wasm.InstrKind) bool {
	return kind >= wasm.InstrI32DivS && kind <= wasm.InstrI32RemU || kind >= wasm.InstrI64DivS && kind <= wasm.InstrI64RemU
}

func comparison(kind wasm.InstrKind) bool {
	return kind == wasm.InstrI32Eqz || kind == wasm.InstrI64Eqz || kind >= wasm.InstrI32Eq && kind <= wasm.InstrF64Ge
}

func VerifyRule(id RuleID, target TargetMask) bool {
	return id > RuleInvalid && int(id) < len(Rules) && Rules[id].ID == id && Rules[id].Verified && Rules[id].Targets&target != 0
}
