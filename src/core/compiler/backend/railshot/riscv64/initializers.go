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

func admitI32Offset(expr wasm.Expr, m *wasm.Module, public bool) error {
	return admitMemoryOffset(expr, m, wasm.I32, public)
}
func admitMemoryOffset(expr wasm.Expr, m *wasm.Module, want wasm.ValType, public bool) error {
	if public {
		return admitModuleIntegerInitializer(expr, want, m, m.ImportedGlobalCount()+len(m.Globals))
	}
	if !wasm.EqualValType(want, wasm.I32) {
		return unsupported("private memory64 initializer")
	}
	if len(expr.Instrs) != 0 {
		return unsupported("i32 offset must be byte-backed")
	}
	_, err := plainConst(expr, false)
	return err
}

// An exact immutable global read resolves within its construction-time scope.
func admitGlobalExpression(expr wasm.Expr, m *wasm.Module, expected wasm.ValType, limit int) error {
	if len(expr.Instrs) != 0 || len(expr.BodyBytes) < 3 || len(expr.BodyBytes) > 7 || expr.BodyBytes[0] != 0x23 {
		return unsupported("bounded byte-backed global expression required")
	}
	r := wasm.ReaderFrom(expr.BodyBytes[1:])
	index, err := r.U32()
	if err != nil || uint64(index) >= uint64(limit) || limit < 0 || limit > m.ImportedGlobalCount()+len(m.Globals) {
		return unsupported("initializer available global index")
	}
	end, err := r.Byte()
	if err != nil || end != 0x0b || r.HasNext() {
		return unsupported("initializer global expression")
	}
	typ, ok := m.GlobalTypeByIndex(index)
	if !ok || typ.Mutable || !wasm.EqualValType(typ.Type, expected) {
		return unsupported("initializer requires exact immutable global")
	}
	return nil
}
