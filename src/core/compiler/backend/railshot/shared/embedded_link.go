package shared

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type EmbeddedNamedModule struct {
	Name   string
	Module *EmbeddedModule
}

type EmbeddedLinkBinding struct {
	ConsumerModule int
	ImportIndex    int
	ProviderModule int
	ExportIndex    int
}

type EmbeddedLinkPlan struct {
	Modules  []EmbeddedNamedModule
	Bindings []EmbeddedLinkBinding
}

// ResolveEmbeddedLinks resolves every retained import by module/name and checks
// its complete embedded ABI contract. It does not publish target addresses;
// firmware layout consumes the returned bindings only after all imports pass.
func ResolveEmbeddedLinks(modules []EmbeddedNamedModule) (*EmbeddedLinkPlan, error) {
	byName := make(map[string]int, len(modules))
	exports := make([]map[string]int, len(modules))
	for i := range modules {
		named := &modules[i]
		if named.Name == "" || named.Module == nil {
			return nil, fmt.Errorf("embedded32: linked module %d has no name or image", i)
		}
		if previous, ok := byName[named.Name]; ok {
			return nil, fmt.Errorf("embedded32: linked modules %d and %d share name %q", previous, i, named.Name)
		}
		byName[named.Name] = i
		exports[i] = make(map[string]int, len(named.Module.Exports))
		for j := range named.Module.Exports {
			name := named.Module.Exports[j].Name
			if _, ok := exports[i][name]; ok {
				return nil, fmt.Errorf("embedded32: module %q has duplicate export %q", named.Name, name)
			}
			exports[i][name] = j
		}
	}
	bindings := make([]EmbeddedLinkBinding, 0)
	for consumerIndex := range modules {
		consumer := modules[consumerIndex].Module
		for importIndex := range consumer.Imports {
			in := &consumer.Imports[importIndex]
			providerIndex, ok := byName[in.Module]
			if !ok {
				return nil, fmt.Errorf("embedded32: module %q import %q.%q has no provider", modules[consumerIndex].Name, in.Module, in.Name)
			}
			exportIndex, ok := exports[providerIndex][in.Name]
			if !ok {
				return nil, fmt.Errorf("embedded32: module %q import %q.%q has no export", modules[consumerIndex].Name, in.Module, in.Name)
			}
			out := &modules[providerIndex].Module.Exports[exportIndex]
			if err := matchEmbeddedImport(in, modules[providerIndex].Module, out); err != nil {
				return nil, fmt.Errorf("embedded32: module %q import %q.%q: %w", modules[consumerIndex].Name, in.Module, in.Name, err)
			}
			bindings = append(bindings, EmbeddedLinkBinding{
				ConsumerModule: consumerIndex,
				ImportIndex:    importIndex,
				ProviderModule: providerIndex,
				ExportIndex:    exportIndex,
			})
		}
	}
	return &EmbeddedLinkPlan{Modules: append([]EmbeddedNamedModule(nil), modules...), Bindings: bindings}, nil
}

func matchEmbeddedImport(in *EmbeddedImport, provider *EmbeddedModule, out *EmbeddedExport) error {
	if in == nil || provider == nil || out == nil || in.Kind != out.Kind {
		return fmt.Errorf("kind mismatch")
	}
	switch in.Kind {
	case wasm.ExternFunc:
		if uint64(out.Index) >= uint64(len(provider.FunctionSignatures)) {
			return fmt.Errorf("function export index %d is unavailable", out.Index)
		}
		signature := &provider.FunctionSignatures[out.Index]
		if in.FunctionTypeID != signature.TypeID || !embeddedValTypesEqual(in.Params, signature.Params) || !embeddedValTypesEqual(in.Results, signature.Results) {
			return fmt.Errorf("function signature mismatch")
		}
	case wasm.ExternTable:
		tables := embeddedModuleTables(provider)
		if uint64(out.Index) >= uint64(len(tables)) {
			return fmt.Errorf("table export index %d is unavailable", out.Index)
		}
		expected, expectedOK := embeddedReferenceValueType(in.Reference)
		actual, actualOK := embeddedReferenceValueType(tables[out.Index].Reference)
		if !expectedOK || !actualOK || expected != actual || !embeddedLimitsMatch(in.Minimum, in.Maximum, in.HasMaximum, tables[out.Index].Minimum, tables[out.Index].Maximum, tables[out.Index].HasMaximum) {
			return fmt.Errorf("table type mismatch")
		}
	case wasm.ExternMem:
		if out.Index != 0 || provider.Memory == nil {
			return fmt.Errorf("memory export index %d is unavailable", out.Index)
		}
		if !embeddedLimitsMatch(in.Minimum, in.Maximum, in.HasMaximum, provider.Memory.Minimum, provider.Memory.Maximum, provider.Memory.HasMaximum) {
			return fmt.Errorf("memory type mismatch")
		}
	case wasm.ExternGlobal:
		typ, mutable, ok := embeddedModuleGlobalType(provider, out.Index)
		if !ok {
			return fmt.Errorf("global export index %d is unavailable", out.Index)
		}
		if !wasm.EqualValType(in.Type, typ) || in.Mutable != mutable {
			return fmt.Errorf("global type mismatch")
		}
	default:
		return fmt.Errorf("kind %d is not linkable", in.Kind)
	}
	return nil
}

func embeddedLimitsMatch(expectedMin, expectedMax uint32, expectedHasMax bool, actualMin, actualMax uint32, actualHasMax bool) bool {
	if actualMin < expectedMin {
		return false
	}
	return !expectedHasMax || actualHasMax && actualMax <= expectedMax
}

func embeddedValTypesEqual(a, b []wasm.ValType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !wasm.EqualValType(a[i], b[i]) {
			return false
		}
	}
	return true
}

func embeddedModuleGlobalType(module *EmbeddedModule, index uint32) (wasm.ValType, bool, bool) {
	if module == nil {
		return wasm.ValType{}, false, false
	}
	if uint64(index) < uint64(len(module.ImportedGlobals)) {
		global := module.ImportedGlobals[index]
		return global.Type, global.Mutable, true
	}
	local := uint64(index) - uint64(len(module.ImportedGlobals))
	if local >= uint64(len(module.Globals)) {
		return wasm.ValType{}, false, false
	}
	global := module.Globals[local]
	return global.Type, global.Mutable, true
}
