//go:build !linux || !riscv64 || tinygo

package wago

func instructionNativeCustomBinding(fn slotHostFunc, _, _ []ValType) any { return fn }
