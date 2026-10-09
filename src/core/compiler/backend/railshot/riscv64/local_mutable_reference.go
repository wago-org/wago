//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Mutable local reference cells have no foreign descriptor ingress: no public table,
// imported reference global, callable reference signature or exported mutable cell. Internal control types
// cannot introduce foreign descriptors.
// Every nonnull value therefore names this instance's canonical directory.
func admitLocalMutableFunctionGlobals(m *wasm.Module) (bool, error) {
	has := false
	for _, g := range m.Globals {
		has = has || g.Type.Mutable && wasm.EqualValType(g.Type.Type, wasm.FuncRef)
	}
	if !has {
		return false, nil
	}
	if len(m.Types) > MaxFunctions || len(m.Exports) > MaxFunctions {
		return false, unsupported("local mutable references require bounded table-free private cells")
	}
	for _, im := range m.Imports {
		if im.Type.Kind == wasm.ExternGlobal && !swarCarrierType(im.Type.GlobalType().Type) {
			return false, unsupported("local mutable references exclude imported reference globals")
		}
	}
	// Initializer admission reaches this helper before ordinary staging budgets.
	// Reference-bearing bindings require the complete bounded confinement proof,
	// including dead descriptor escapes, not an unexported-function heuristic.
	privateFunctions, err := hasDefinedReferenceFunctions(m)
	if err != nil {
		return false, err
	}
	if privateFunctions {
		// The two bounded proofs preserve scalar256 and SWAR128 limits without
		// running moduleUsesSWAR before its byte budget. Staging repeats the
		// precise proof; success here selects no execution ABI or carrier mode.
		if err := admitClosedReferenceFunctionModule(m, false); err != nil {
			if err := admitClosedReferenceFunctionModule(m, true); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	if len(m.Tables) != 0 || m.ImportedTableCount() != 0 {
		return false, unsupported("local mutable references require bounded table-free private cells")
	}
	// Enclosing source admission checks every declared type and its profile
	// bounds. Only actual callable bindings can introduce foreign descriptors;
	// internal reference-bearing control declarations cannot do so.
	for i := 0; i < m.FuncCount(); i++ {
		sig, ok := m.FuncSignature(uint32(i))
		if !ok || sig.Kind != wasm.CompFunc || len(sig.Params) > 256 || len(sig.Results) > 256 {
			return false, unsupported("local mutable reference function binding")
		}
		for _, list := range [][]wasm.ValType{sig.Params, sig.Results} {
			for _, typ := range list {
				if !swarCarrierType(typ) {
					return false, unsupported("local mutable references exclude callable reference signatures")
				}
			}
		}
	}
	for _, ex := range m.Exports {
		if ex.Index.Kind != wasm.ExternGlobal {
			continue
		}
		typ, ok := m.GlobalTypeByIndex(ex.Index.Index)
		if !ok {
			return false, unsupported("local mutable reference export index")
		}
		if typ.Mutable && wasm.EqualValType(typ.Type, wasm.FuncRef) {
			return false, unsupported("mutable reference global export is pending")
		}
	}
	return true, nil
}

func admitLocalFunctionGlobal(g wasm.Global, m *wasm.Module, limit int, mutable bool) error {
	if g.Type.Mutable {
		if !mutable {
			return unsupported("mutable reference global requires closed local profile")
		}
		// Reuse only the initializer rules. Export admission still calls the
		// immutable helper directly, so this does not permit mutable aliases.
		g.Type.Mutable = false
	}
	return admitImmutableFunctionGlobal(g, m, limit)
}
