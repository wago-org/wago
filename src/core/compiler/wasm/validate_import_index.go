// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

// One compact index array covers every import kind. It is built before parallel
// validation and is immutable thereafter. Invalid kinds remain validator errors.
func (v *moduleValidator) ensureImportIndexes() {
	if v.importIndexReady {
		return
	}
	var counts [5]int
	for i := range v.m.Imports {
		kind := v.m.Imports[i].Type.Kind
		if int(kind) < len(counts) {
			counts[kind]++
		}
	}
	storage := make([]uint32, len(v.m.Imports))
	for kind, count := range counts {
		v.importIndexes[kind] = storage[:0:count]
		storage = storage[count:]
	}
	for i := range v.m.Imports {
		kind := v.m.Imports[i].Type.Kind
		if int(kind) < len(counts) {
			v.importIndexes[kind] = append(v.importIndexes[kind], uint32(i))
		}
	}
	v.importIndexReady = true
}

func (v *moduleValidator) importsOfKind(kind ExternKind) []uint32 {
	v.ensureImportIndexes()
	return v.importIndexes[kind]
}
