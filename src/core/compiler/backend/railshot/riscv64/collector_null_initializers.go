//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Null collector globals need no object allocation, collector domain or native
// root ABI. The public decoder separately validates their declared heap types.
func abstractCollectorNullInitializer(global wasm.Global) bool {
	typ := global.Type.Type
	if typ.Kind() != wasm.ValRef || !typ.Ref().Nullable() || typ.Ref().Exact() || typ.Ref().Heap().Kind() != wasm.HeapAbs {
		return false
	}
	heap := typ.Ref().Heap().Abs()
	switch heap {
	case wasm.HeapNone, wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray:
	default:
		return false
	}
	if len(global.Init.Instrs) != 0 || len(global.Init.BodyBytes) > 8 {
		return false
	}
	r := wasm.NewReader(global.Init.BodyBytes)
	op, err := r.Byte()
	if err != nil || op != 0xd0 {
		return false
	}
	actual, err := r.S33()
	if err != nil || actual != int64(byte(heap))-128 && actual != int64(byte(wasm.HeapNone))-128 {
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && !r.HasNext()
}
