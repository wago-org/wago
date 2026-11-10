// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package shared

import (
	"reflect"
	"testing"
)

func TestResidencyStatsRemainPointerFree(t *testing.T) {
	typ := reflect.TypeOf(ResidencyStats{})
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).Type.Kind() == reflect.Pointer || typ.Field(i).Type.Kind() == reflect.Map || typ.Field(i).Type.Kind() == reflect.Slice || typ.Field(i).Type.Kind() == reflect.Interface {
			t.Fatalf("field %s has pointer-bearing kind %s", typ.Field(i).Name, typ.Field(i).Type.Kind())
		}
	}
	if (ResidencyStats{}).Active() {
		t.Fatal("zero stats reported active")
	}
	if !(ResidencyStats{Candidates: 1}).Active() {
		t.Fatal("nonzero stats reported inactive")
	}
}
