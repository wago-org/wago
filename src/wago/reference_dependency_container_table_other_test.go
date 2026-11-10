// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !riscv64 && !tinygo

package wago

import (
	"testing"
	"unsafe"
)

func TestTableDescriptorDependenciesExistingTargetFootprint(t *testing.T) {
	if unsafe.Sizeof(tableDescriptorDependencies{}) != 0 || unsafe.Sizeof(Table{}) != 64 || unsafe.Offsetof(tableOwner{}.mu) != 0 {
		t.Fatal("existing-target table descriptor sidecar changed object footprint")
	}
}
