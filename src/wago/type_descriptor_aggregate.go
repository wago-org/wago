// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (c wasmTypeDescriptorConverter) lowerAggregateTypeFields(d *DefinedTypeDescriptor, comp *wasm.CompType, sourceGroup int) error {
	if comp.Kind == wasm.CompArray {
		d.Kind = CompositeTypeArray
		field, err := c.fieldType(comp.Array, sourceGroup)
		if err != nil {
			return wrapContextError("array field", err)
		}
		d.Array = field
		return nil
	}

	d.Kind = CompositeTypeStruct
	d.Fields = make([]FieldTypeDescriptor, len(comp.Fields))
	for i := range comp.Fields {
		field, err := c.fieldType(comp.Fields[i], sourceGroup)
		if err != nil {
			return fmt.Errorf("field %d: %w", i, err)
		}
		d.Fields[i] = field
	}
	return nil
}

func (c wasmTypeDescriptorConverter) fieldType(f wasm.FieldType, group int) (FieldTypeDescriptor, error) {
	out := FieldTypeDescriptor{Mutable: f.Mut() == wasm.Var}
	if f.Storage().Packed() {
		out.Storage.Packed = true
		switch f.Storage().Pack() {
		case wasm.PackI8:
			out.Storage.PackedType = PackedTypeI8
		case wasm.PackI16:
			out.Storage.PackedType = PackedTypeI16
		default:
			return out, fmt.Errorf("unknown packed type 0x%x", byte(f.Storage().Pack()))
		}
		return out, nil
	}
	v, err := c.valueType(f.Storage().Val(), group)
	if err != nil {
		return out, err
	}
	out.Storage.Value = v
	return out, nil
}

func validateAggregateTypeFields(d *DefinedTypeDescriptor, index int, types []DefinedTypeDescriptor) error {
	if d.Kind == CompositeTypeArray {
		if len(d.Params) != 0 || len(d.Results) != 0 || len(d.Fields) != 0 {
			return fmt.Errorf("compiled metadata invalid: array type %d carries non-array metadata", index)
		}
		return validateAggregateField(fmt.Sprintf("type %d array field", index), d.Array, types)
	}
	if len(d.Params) != 0 || len(d.Results) != 0 || d.Array != (FieldTypeDescriptor{}) {
		return fmt.Errorf("compiled metadata invalid: struct type %d carries non-field metadata", index)
	}
	for j, field := range d.Fields {
		if err := validateAggregateField(fmt.Sprintf("type %d field %d", index, j), field, types); err != nil {
			return err
		}
	}
	return nil
}

func validateAggregateField(context string, f FieldTypeDescriptor, types []DefinedTypeDescriptor) error {
	if !f.Storage.Packed {
		return validateValueTypeDescriptor(context, f.Storage.Value, types)
	}
	if f.Storage.PackedType > PackedTypeI16 {
		return fmt.Errorf("compiled metadata invalid: %s packed type %d invalid", context, f.Storage.PackedType)
	}
	if f.Storage.Value != (ValueTypeDescriptor{}) {
		return fmt.Errorf("compiled metadata invalid: %s packed field carries value metadata", context)
	}
	return nil
}

func (w *compiledWriter) aggregateTypeFields(d *DefinedTypeDescriptor) {
	if d.Kind == CompositeTypeArray {
		w.fieldType(d.Array)
		return
	}
	w.uvar(uint64(len(d.Fields)))
	for _, field := range d.Fields {
		w.fieldType(field)
	}
}

func (w *compiledWriter) fieldType(f FieldTypeDescriptor) {
	w.bool(f.Storage.Packed)
	if f.Storage.Packed {
		w.u8(byte(f.Storage.PackedType))
	} else {
		w.valueType(f.Storage.Value)
	}
	w.bool(f.Mutable)
}
