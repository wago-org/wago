//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Count source namespaces directly before any cached import-directory allocation.
// A hand-built module may have changed in place since its directory was cached.
type closedReferenceImports struct{ functions, globals, memories int }

func countClosedReferenceImports(m *wasm.Module) (closedReferenceImports, error) {
	var counts closedReferenceImports
	if m == nil || len(m.Imports) > MaxProductionImports {
		return counts, unsupported("closed reference import directory bound")
	}
	for _, im := range m.Imports {
		switch im.Type.Kind {
		case wasm.ExternFunc:
			counts.functions++
		case wasm.ExternGlobal:
			counts.globals++
		case wasm.ExternMem:
			counts.memories++
		default:
			return counts, unsupported("closed reference numeric imports only")
		}
	}
	if counts.functions > MaxHostFunctionImports || len(m.Code) > MaxFunctions-counts.functions || len(m.Globals) > 256-counts.globals || len(m.Memories) > MaxProductionMemories-counts.memories {
		return counts, unsupported("closed reference import namespace bound")
	}
	return counts, nil
}

// Imported storage remains numeric; owned reference cells contain only
// null or this instance's numeric descriptor slots. Their
// initializers and payloads share the closed module's aggregate byte budget,
// before validation, copying data, or allocating derived state directories.
func admitClosedReferenceNumericState(m *wasm.Module, vector bool, remaining *int) error {
	counts, err := countClosedReferenceImports(m)
	if err != nil {
		return err
	}
	return admitClosedReferenceNumericStateWithImports(m, vector, remaining, counts)
}

func admitClosedReferenceNumericStateWithImports(m *wasm.Module, vector bool, remaining *int, counts closedReferenceImports) error {
	if len(m.Data) > 256 || m.DataCount != nil && *m.DataCount != uint32(len(m.Data)) {
		return unsupported("closed reference numeric state directory bound")
	}
	consume := func(n int) bool {
		if n > *remaining {
			return false
		}
		*remaining -= n
		return true
	}
	for _, im := range m.Imports {
		switch im.Type.Kind {
		case wasm.ExternFunc:
			// Function bindings are checked with declared control types.
		case wasm.ExternGlobal:
			g := im.Type.GlobalType()
			if im.Type != wasm.NewGlobalExternType(g) || g.Type.Kind() == wasm.ValRef || !scalarType(g.Type, true) && !(vector && wasm.EqualValType(g.Type, wasm.V128)) || !consume(2) {
				return unsupported("closed reference numeric global import")
			}
		case wasm.ExternMem:
			memory := im.Type.MemType()
			if im.Type != wasm.NewMemExternType(memory) || !memory.Limits.HasMax && memory.Limits.Max != 0 {
				return unsupported("closed reference canonical memory import")
			}
			if err := admitMemoryType(memory, true, true); err != nil {
				return err
			}
			// Include the separator retained in the exact import key.
			if !consume(22) {
				return unsupported("closed reference memory import bytes")
			}
		default:
			return unsupported("closed reference numeric imports only")
		}
	}
	for _, memory := range m.Memories {
		if !memory.Limits.HasMax && memory.Limits.Max != 0 {
			return unsupported("closed reference inactive memory maximum")
		}
		if err := admitMemoryType(memory, true, true); err != nil {
			return err
		}
		if !consume(21) {
			return unsupported("closed reference memory declaration bytes")
		}
	}
	for _, global := range m.Globals {
		typ := global.Type.Type
		_, indexedReference := privateIndexedFunctionGlobal(global, m)
		reference := wasm.EqualValType(typ, wasm.FuncRef) || indexedReference
		if !reference && (typ.Kind() == wasm.ValRef || !scalarType(typ, true) && !(vector && wasm.EqualValType(typ, wasm.V128))) {
			return unsupported("closed reference owned numeric or private funcref globals only")
		}
		if reference {
			// This parser is bounded and never calls the local-mutable helper:
			// early initializer approval may itself call the closed-module proof.
			if _, err := tableElementExpression(global.Init); err != nil {
				return err
			}
		}
		if len(global.Init.Instrs) != 0 || len(global.Init.BodyBytes) == 0 || len(global.Init.BodyBytes) > MaxIntegerInitializerBytes || !consume(2+len(global.Init.BodyBytes)) {
			return unsupported("closed reference global initializer bytes")
		}
	}
	if m.DataCount != nil && !consume(5) {
		return unsupported("closed reference data count bytes")
	}
	for _, data := range m.Data {
		if len(data.Mode.Offset.Instrs) != 0 {
			return unsupported("closed reference byte-backed data offset")
		}
		switch data.Mode.Kind {
		case wasm.DataActive:
			if uint64(data.Mode.Mem) >= uint64(counts.memories+len(m.Memories)) || len(data.Mode.Offset.BodyBytes) == 0 || len(data.Mode.Offset.BodyBytes) > MaxIntegerInitializerBytes {
				return unsupported("closed reference active data shape")
			}
		case wasm.DataPassive:
			if data.Mode.Mem != 0 || len(data.Mode.Offset.BodyBytes) != 0 {
				return unsupported("closed reference inactive passive data metadata")
			}
		default:
			return unsupported("closed reference data mode")
		}
		if !consume(4+len(data.Mode.Offset.BodyBytes)) || !consume(len(data.Init)) {
			return unsupported("closed reference data payload bytes")
		}
	}
	return nil
}
