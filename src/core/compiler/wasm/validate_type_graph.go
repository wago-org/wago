// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

import "github.com/wago-org/wago/internal/buildfeatures"

// ValidateTypeGraph validates the recursive type declarations in m. Native
// metadata admission uses this when rebuilding a type graph for structural
// keys. Full Wasm input still requires ValidateModule.
func ValidateTypeGraph(m *Module) error {
	v := moduleValidator{m: m, funcIndex: -1}
	if m == nil {
		return v.err(ErrTypeMismatch, "nil module")
	}
	m.invalidateTypeAnalysisCaches()
	return v.validateTypeGraph()
}

func (v *moduleValidator) validateTypeGraph() error {
	for gi, rt := range v.m.Types {
		for si, st := range rt.SubTypes {
			if buildfeatures.GC != 0 {
				if err := v.validateDeclaredSubtype(&st, gi, si); err != nil {
					return err
				}
			} else if st.HasPrefix || len(st.Supers) != 0 || st.Metadata.Describes.Present() || st.Metadata.Descriptor.Present() {
				return v.err(ErrUnsupportedFeature, "gc is disabled in this build")
			}
			if err := v.validateCompTypeInRecGroup(st.Comp, gi); err != nil {
				return err
			}
		}
	}
	if buildfeatures.GC != 0 {
		if err := v.validateSubtypeMetadata(); err != nil {
			return err
		}
	}
	return nil
}
