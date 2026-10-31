//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// admitSWARDecodedMetadataBudget accounts only storage and traversal bounds.
// Names never select instructions, optimizations or compilation paths. Preserve
// encoded/decoded accounting for hand-built callers as well as parsed modules.
func admitSWARDecodedMetadataBudget(m *wasm.Module, remaining *int) error {
	consume := func(n int) bool {
		if n > *remaining {
			return false
		}
		*remaining -= n
		return true
	}
	rawNameAccounted := false
	for _, custom := range m.Customs {
		if !consume(len(custom.Name)) || !consume(len(custom.Data)) {
			return unsupported("SWAR decoded custom byte bound")
		}
		if custom.Name == "name" && bytes.Equal(custom.Data, m.RawNameSecPayload) {
			rawNameAccounted = true
		}
	}
	for _, im := range m.Imports {
		if !consume(len(im.Module)) || !consume(len(im.Name)) {
			return unsupported("SWAR decoded import name byte bound")
		}
	}
	for _, export := range m.Exports {
		if !consume(len(export.Name)) {
			return unsupported("SWAR decoded export name byte bound")
		}
	}
	if !rawNameAccounted && !consume(len(m.RawNameSecPayload)) {
		return unsupported("SWAR decoded name metadata byte bound")
	}
	if names := m.NameSec; names != nil {
		// Account decoded maps/strings as well as encoded payloads. Hand-built
		// callers may provide either representation, or inconsistent copies.
		// Small entry overhead also bounds work for arbitrary empty names.
		nameMap := func(rows wasm.NameMap, limit int) bool {
			if len(rows) > limit {
				return false
			}
			for _, row := range rows {
				if !consume(2) || !consume(len(row.Name)) {
					return false
				}
			}
			return true
		}
		if names.ModuleName != nil && !consume(len(*names.ModuleName)) {
			return unsupported("SWAR decoded module name byte bound")
		}
		for _, rows := range []wasm.NameMap{names.FunctionNames, names.TableNames, names.MemoryNames, names.GlobalNames, names.ElementNames, names.DataNames, names.TagNames} {
			if !nameMap(rows, MaxFunctions) {
				return unsupported("SWAR decoded name map byte/count bound")
			}
		}
		if !nameMap(names.TypeNames, MaxTypeDeclarations) {
			return unsupported("SWAR decoded type name byte/count bound")
		}
		for _, groups := range []wasm.IndirectNameMap{names.LocalNames, names.LabelNames} {
			if len(groups) > MaxFunctions {
				return unsupported("SWAR decoded indirect name count bound")
			}
			for _, group := range groups {
				if !consume(2) || !nameMap(group.Names, MaxFunctions) {
					return unsupported("SWAR decoded indirect name byte/count bound")
				}
			}
		}
		if len(names.FieldNames) > MaxTypeDeclarations {
			return unsupported("SWAR decoded field name count bound")
		}
		for _, group := range names.FieldNames {
			if !consume(2) || !nameMap(group.Names, MaxTypeDeclarations) {
				return unsupported("SWAR decoded field name byte/count bound")
			}
		}
	}
	return nil
}
