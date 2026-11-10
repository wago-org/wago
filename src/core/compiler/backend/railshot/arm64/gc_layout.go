// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (f *fn) gcTypeLayout(typeIndex uint32, kind wasm.CompTypeKind) (codegen.GCTypeLayout, bool) {
	if int(typeIndex) >= len(f.gcTypeLayouts) {
		return codegen.GCTypeLayout{}, false
	}
	layout := f.gcTypeLayouts[typeIndex]
	return layout, layout.Type != nil && layout.Type.Comp.Kind == kind
}

func (f *fn) stagedStructType(typeIndex uint32) (wasm.SubType, bool) {
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct); ok {
		return *layout.Type, true
	}
	return stagedStructType(f.m, typeIndex)
}

func (f *fn) stagedStructField(typeIndex, fieldIndex uint32) (wasm.FieldType, bool) {
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct); ok {
		if int(fieldIndex) >= len(layout.Type.Comp.Fields) {
			return wasm.FieldType{}, false
		}
		return layout.Type.Comp.Fields[fieldIndex], true
	}
	return stagedStructField(f.m, typeIndex, fieldIndex)
}

func (f *fn) stagedArraySubtype(typeIndex uint32) (wasm.SubType, bool) {
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray); ok {
		return *layout.Type, true
	}
	return stagedArraySubtype(f.m, typeIndex)
}

func (f *fn) stagedArrayType(typeIndex uint32) (wasm.FieldType, bool) {
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray); ok {
		return layout.Type.Comp.Array, true
	}
	return stagedArrayType(f.m, typeIndex)
}
