//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Numeric owned storage cannot contain a function-reference carrier. Its
// initializers and payloads share the closed module's aggregate byte budget,
// before validation, copying data, or allocating derived state directories.
func admitClosedReferenceNumericState(m *wasm.Module, vector bool, remaining *int) error {
	if len(m.Memories) > MaxProductionMemories || len(m.Globals) > 256 || len(m.Data) > 256 || m.DataCount != nil && *m.DataCount != uint32(len(m.Data)) {
		return unsupported("closed reference numeric state directory bound")
	}
	consume := func(n int) bool {
		if n > *remaining {
			return false
		}
		*remaining -= n
		return true
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
		if typ.Kind() == wasm.ValRef || !scalarType(typ, true) && !(vector && wasm.EqualValType(typ, wasm.V128)) {
			return unsupported("closed reference owned numeric globals only")
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
			if uint64(data.Mode.Mem) >= uint64(len(m.Memories)) || len(data.Mode.Offset.BodyBytes) == 0 || len(data.Mode.Offset.BodyBytes) > MaxIntegerInitializerBytes {
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
