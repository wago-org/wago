//go:build linux && riscv64 && !tinygo

package wago

func instructionNativeCustomBinding(fn slotHostFunc, params, results []ValType) any {
	return nativeCustomInstructionBinding{fn: fn, sig: FuncSig{Params: append([]ValType(nil), params...), Results: append([]ValType(nil), results...)}}
}
