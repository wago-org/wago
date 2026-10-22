//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// AdmitTypeDeclarations identifies metadata with no executable obligations.
// Full frontend type validation is still required before compilation.
func AdmitTypeDeclarations(m *wasm.Module) bool { return admitPublicTypeDeclarations(m) }

// A declaration-only module retains the frontend-validated graph but has no
// native frames, reference carriers, resource storage or executable start.
func admitPublicTypeDeclarations(m *wasm.Module) bool {
	if m == nil || len(m.FuncTypes) != 0 || len(m.Code) != 0 || len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Globals) != 0 || len(m.Exports) != 0 || m.Start != nil || len(m.Elements) != 0 || len(m.Data) != 0 {
		return false
	}
	for _, group := range m.Types {
		for _, sub := range group.SubTypes {
			if sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc && sub.Comp.Kind != wasm.CompStruct && sub.Comp.Kind != wasm.CompArray {
				return false
			}
		}
	}
	return true
}

// AdmitPrimitiveFunctionGraphModule identifies primitive executable carriers
// with no collector operations, including initializers. Indexed function tests
// and casts use canonical descriptors. Inert aggregate declarations
// remain part of the exact recursive graph and create no collector demand.
// The caller already holds the validated source graph; lowering still checks
// its full carrier, instruction, descriptor and execution contracts.
func AdmitPrimitiveFunctionGraphModule(m *wasm.Module) bool {
	if !admitPublicPrimitiveReferenceModule(m, true) {
		return false
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	plain := func(body []byte) bool {
		r := wasm.ReaderFrom(body)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return false
			}
			var immediate wasm.InstructionImmediate
			if classifier.ClassifyInto(&r, op, &immediate) != nil {
				return false
			}
			if op == 0xfb {
				if (immediate.Kind != wasm.InstrRefTest && immediate.Kind != wasm.InstrRefCast) || !immediate.HasHeapType || immediate.HeapType.Kind() != wasm.HeapTypeIndex || immediate.HeapType.Type().Rec {
					return false
				}
				if _, function := m.TypeFunc(immediate.HeapType.Type().Index); !function {
					return false
				}
			}
		}
		return true
	}
	for _, function := range m.Code {
		if !plain(function.BodyBytes) {
			return false
		}
	}
	for _, global := range m.Globals {
		if !plain(global.Init.BodyBytes) {
			return false
		}
	}
	for _, table := range m.Tables {
		if table.Init != nil && !plain(table.Init.BodyBytes) {
			return false
		}
	}
	for _, element := range m.Elements {
		if !plain(element.Mode.Offset.BodyBytes) {
			return false
		}
		for _, expression := range element.Kind.Exprs {
			if !plain(expression.BodyBytes) {
				return false
			}
		}
	}
	for _, data := range m.Data {
		if !plain(data.Mode.Offset.BodyBytes) {
			return false
		}
	}
	return true
}
