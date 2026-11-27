// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package frontend

import (
	"fmt"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// BuildCompileTypeMetadata retains function type IDs even when the compiler
// has no reference-signature or collector lowering. Numeric indirect calls
// still depend on those IDs matching the flattened Wasm type order.
func BuildCompileTypeMetadata(m *wasm.Module, opts GCTypeMetadataOptions) (GCTypeMetadata, error) {
	if buildfeatures.ReferenceSignatures {
		return BuildGCTypeMetadataWithOptions(m, opts)
	}
	if m == nil {
		return GCTypeMetadata{}, fmt.Errorf("frontend: nil wasm module")
	}
	count := 0
	for _, group := range m.Types {
		count += len(group.SubTypes)
	}
	descs := make([]gc.TypeDesc, count)
	index := 0
	for _, group := range m.Types {
		for _, sub := range group.SubTypes {
			if sub.Comp.Kind != wasm.CompFunc || len(sub.Supers) != 0 {
				return GCTypeMetadata{}, fmt.Errorf("frontend: type %d requires reference type lowering", index)
			}
			descs[index] = gc.TypeDesc{ID: gc.TypeID(index), Kind: gc.KindFunc, Final: sub.Final}
			index++
		}
	}
	return GCTypeMetadata{Descs: descs}, nil
}

// ValidateCompileTypeDescs checks retained descriptors against the compiler's
// build profile before backend or runtime admission.
func ValidateCompileTypeDescs(descs []gc.TypeDesc) error {
	if !buildfeatures.ReferenceSignatures {
		return gc.ValidatePlainFunctionTypeDescs(descs)
	}
	return validateLoweredTypeDescs(descs)
}

func validateLoweredTypeDescs(descs []gc.TypeDesc) error {
	if buildfeatures.GC != 0 {
		return gc.ValidateTypeDescs(descs)
	}
	return gc.ValidateFunctionTypeDescs(descs)
}
