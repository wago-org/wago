//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Ordinary signatures borrow validated module storage. Only recursive-local
// heap indexes need normalized signatures, held once per flattened type.
// The original recursive groups, supers and type identities stay unchanged.
type sourceFunctionTypeDirectory struct {
	module            *wasm.Module
	foreignFunctions  bool
	localNumericTails bool // no callable input and only numeric function types
	normalized        []*wasm.CompType
	storage           []wasm.CompType
}

func sourceNeedsFunctionTypeDirectory(m *wasm.Module) bool {
	for _, group := range m.Types {
		if len(group.SubTypes) != 1 {
			return true
		}
		for index := range group.SubTypes {
			sub := &group.SubTypes[index]
			if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Comp.Kind == wasm.CompFunc && sourceFunctionTypeNeedsResolution(&sub.Comp) {
				return true
			}
		}
	}
	return false
}

func sourceFunctionTypeNeedsResolution(typ *wasm.CompType) bool {
	for _, values := range [][]wasm.ValType{typ.Params, typ.Results} {
		for _, value := range values {
			if value.Kind() == wasm.ValRef && value.Ref().Heap().Kind() == wasm.HeapTypeIndex && value.Ref().Heap().Type().Rec {
				return true
			}
		}
	}
	return false
}

func newSourceFunctionTypeDirectory(m *wasm.Module) (sourceFunctionTypeDirectory, error) {
	d := sourceFunctionTypeDirectory{module: m, foreignFunctions: sourceMayReceiveForeignFunctions(m)}
	if m == nil {
		return d, unsupported("nil source type directory")
	}
	d.localNumericTails = !d.foreignFunctions && m.ImportedFuncCount() == 0
	count, resolved := 0, 0
	for _, group := range m.Types {
		if len(group.SubTypes) > MaxPublicCodeBytes/8-count {
			return d, unsupported("source type directory storage bound")
		}
		count += len(group.SubTypes)
		for index := range group.SubTypes {
			typ := &group.SubTypes[index].Comp
			if d.localNumericTails && typ.Kind == wasm.CompFunc {
				for _, values := range [][]wasm.ValType{typ.Params, typ.Results} {
					for _, value := range values {
						if value.Kind() == wasm.ValRef {
							d.localNumericTails = false
						}
					}
				}
			}
			if typ.Kind == wasm.CompFunc && sourceFunctionTypeNeedsResolution(typ) {
				resolved++
			}
		}
	}
	if resolved == 0 {
		return d, nil
	}
	d.normalized = make([]*wasm.CompType, count)
	d.storage = make([]wasm.CompType, resolved)
	position := 0
	for index := 0; index < count; index++ {
		typ, function := m.TypeFunc(uint32(index))
		if !function || !sourceFunctionTypeNeedsResolution(typ) {
			continue
		}
		if !m.ResolveTypeFunc(uint32(index), &d.storage[position]) {
			return sourceFunctionTypeDirectory{}, unsupported("source function type resolution")
		}
		d.normalized[index] = &d.storage[position]
		position++
	}
	return d, nil
}

func (d sourceFunctionTypeDirectory) function(index uint32) (*wasm.CompType, bool) {
	if uint64(index) < uint64(len(d.normalized)) && d.normalized[index] != nil {
		return d.normalized[index], true
	}
	if d.module == nil {
		return nil, false
	}
	return d.module.TypeFunc(index)
}

func (d sourceFunctionTypeDirectory) key(index uint32) (uint64, bool) {
	if d.module == nil {
		return 0, false
	}
	if _, function := d.function(index); !function {
		return 0, false
	}
	return d.module.StructuralTypeKeyChecked(index)
}

func (d sourceFunctionTypeDirectory) subtypeKeys(index uint32) ([]uint64, bool) {
	if d.module == nil {
		return nil, false
	}
	indexes, ok := d.module.FunctionSubtypeTypeIndexes(index)
	if !ok || len(indexes) == 0 || len(indexes) > MaxFunctions {
		return nil, false
	}
	keys := make([]uint64, len(indexes))
	for i, subtype := range indexes {
		key, ok := d.key(subtype)
		if !ok {
			return nil, false
		}
		keys[i] = key
	}
	return keys, true
}

// NeedsFunctionSubtypeHelpers selects exact owner-backed checks only where a
// nonfinal declared function type can accept subtypes absent from this image.
func NeedsFunctionSubtypeHelpers(m *wasm.Module) bool {
	if m == nil || !sourceMayReceiveForeignFunctions(m) {
		return false
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for _, fn := range m.Code {
		r := wasm.NewReader(fn.BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return false
			}
			var imm wasm.InstructionImmediate
			if classifier.ClassifyInto(r, op, &imm) != nil {
				return false
			}
			index, check := imm.Index, false
			switch imm.Kind {
			case wasm.InstrCallRef, wasm.InstrCallIndirect:
				check = true
			case wasm.InstrRefTest, wasm.InstrRefCast:
				if imm.HasHeapType && imm.HeapType.Kind() == wasm.HeapTypeIndex && !imm.HeapType.Type().Rec {
					index, check = imm.HeapType.Type().Index, true
				}
			}
			if check {
				sub, ok := m.TypeSubType(index)
				if ok && sub.Comp.Kind == wasm.CompFunc && !sub.Final {
					return true
				}
			}
		}
	}
	return false
}

func (d sourceFunctionTypeDirectory) needsSubtypeHelper(index uint32) bool {
	if d.module == nil || !d.foreignFunctions {
		return false
	}
	sub, ok := d.module.TypeSubType(index)
	return ok && sub.Comp.Kind == wasm.CompFunc && !sub.Final
}

// Imported definitions and externally writable/reference-bearing boundaries
// can introduce an actual function type absent from the local declarations.
// Closed numeric modules keep their existing local-key-only instruction bytes.
func sourceMayReceiveForeignFunctions(m *wasm.Module) bool {
	if m == nil {
		return false
	}
	if m.ImportedFuncCount() != 0 || m.ImportedTableCount() != 0 || m.ImportedGlobalCount() != 0 || m.TagCount() != 0 {
		return true
	}
	functionStorage := wasm.ModuleFunctionReferenceStorage(m)
	for _, export := range m.Exports {
		switch export.Index.Kind {
		case wasm.ExternTable:
			table, ok := m.TableType(export.Index.Index)
			if ok && wasm.FunctionReferenceType(m, wasm.RefVal(table.Ref)) {
				return true
			}
		case wasm.ExternGlobal:
			global, ok := m.GlobalTypeByIndex(export.Index.Index)
			if ok && global.Mutable && (wasm.FunctionReferenceType(m, global.Type) || functionStorage && global.Type.Kind() == wasm.ValRef) {
				return true
			}
		case wasm.ExternFunc:
			sig, ok := m.FuncSignature(export.Index.Index)
			if !ok {
				continue
			}
			for _, value := range sig.Params {
				if wasm.FunctionReferenceType(m, value) || functionStorage && value.Kind() == wasm.ValRef {
					return true
				}
			}
		}
	}
	return false
}
