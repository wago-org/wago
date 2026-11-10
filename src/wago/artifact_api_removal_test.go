// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !tinygo && !wago_target_tinygo

package wago

import (
	"reflect"
	"testing"
)

func TestCompiledHasNoArtifactCodec(t *testing.T) {
	typ := reflect.TypeOf((*Compiled)(nil))
	for _, name := range []string{"MarshalBinary", "UnmarshalBinary", "WriteTo", "ReadFrom", "ReadFromWithLimits", "ArtifactSectionSizes"} {
		if _, exists := typ.MethodByName(name); exists {
			t.Errorf("retired artifact method %s is still exposed", name)
		}
	}
}
