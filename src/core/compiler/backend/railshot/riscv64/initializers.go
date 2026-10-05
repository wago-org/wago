//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Production initializer values are resolved by the normal runtime after import
// attachment. The zero returned for global.get is typing metadata only; isolated
// execution cannot admit an initializer needing those production cells.
func admittedScalarConst(expr wasm.Expr, typ wasm.ValType, production bool, imports []globalSpec) (uint64, error) {
	if len(expr.BodyBytes) == 0 || expr.BodyBytes[0] != 0x23 {
		return plainScalarConst(expr, typ, production)
	}
	if !production {
		return 0, unsupported("isolated imported-global initializer")
	}
	r := wasm.ReaderFrom(expr.BodyBytes[1:])
	index, err := r.U32()
	if err != nil || uint64(index) >= uint64(len(imports)) {
		return 0, unsupported("initializer requires an imported global")
	}
	end, err := r.Byte()
	if err != nil || end != 0x0b || r.HasNext() {
		return 0, unsupported("extended imported-global initializer")
	}
	g := imports[index]
	if g.mutable || !wasm.EqualValType(g.typ, typ) || !scalarType(typ, true) {
		return 0, unsupported("initializer requires an immutable imported global of the exact scalar type")
	}
	return 0, nil
}
