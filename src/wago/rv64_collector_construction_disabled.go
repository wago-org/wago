// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux || !riscv64 || tinygo || wago_target_tinygo || (wago_no_gc && wago_no_exception_handling)

package wago

import (
	"fmt"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type managedFunctionConstruction struct{}

func managedFunctionConstructionNeeded(*Compiled) bool { return false }
func managedAggregateHelperFamilies(*Compiled) uint64  { return 0 }

func newManagedFunctionConstruction(*Compiled, *gc.Collector, *gc.Slots) *managedFunctionConstruction {
	return nil
}
func (*managedFunctionConstruction) roots(fallback gc.Slots) gc.RootSet          { return fallback }
func (*managedFunctionConstruction) close()                                      {}
func (*managedFunctionConstruction) bind(*Instance, *gcStoreDomain) error        { return nil }
func recordManagedFunctionConstruction(*gc.Collector, *Compiled, gc.Ref, uint32) {}

type compactExternConstruction struct{}

func compactExternConstructionNeeded(*Compiled) bool { return false }
func newCompactExternConstruction(*Compiled, *gc.Collector, *referenceStore, *gcStoreDomain, *gc.Slots) *compactExternConstruction {
	return nil
}
func (j *compactExternConstruction) roots(roots gc.RootSet) gc.RootSet { return roots }
func (j *compactExternConstruction) convert(uint32, gcConstStackValue, gc.RootSet) (uint64, error) {
	return 0, fmt.Errorf("compact extern constants unavailable")
}
func (j *compactExternConstruction) record([]byte, *Instance) error { return nil }
func (j *compactExternConstruction) bind(*Instance) error           { return nil }
func (j *compactExternConstruction) close()                         {}
