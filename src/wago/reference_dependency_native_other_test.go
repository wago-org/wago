// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !riscv64

package wago

import (
	"testing"
	"unsafe"
)

func TestFunctionDependenciesExistingTargetFootprintAndRoots(t *testing.T) {
	type previous struct{ set importDedup[*Instance] }
	var a functionImportAttachments
	if unsafe.Sizeof(functionDependencyAttachments{}) != 0 || unsafe.Sizeof(a) != unsafe.Sizeof(previous{}) || unsafe.Offsetof(a.set) != 0 {
		t.Fatal("existing-target function attachment footprint")
	}
	producer := &Instance{}
	export := &InstanceExport{inst: producer}
	failed := false
	allocs := testing.AllocsPerRun(100, func() {
		if a.attach(export) != nil || a.attach(export) != nil || producer.resourceRefs != 1 {
			failed = true
		}
		if railshotHasBuilderFunctionDependencies(&a) {
			failed = true
		}
		a.detachAll()
	})
	if failed || allocs != 0 || producer.resourceRefs != 0 || producer.importState.Load() != nil {
		t.Fatal("existing-target anonymous roots", failed, allocs, producer.resourceRefs)
	}
}
