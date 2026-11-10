// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build riscv64

package plugins

import (
	"github.com/wago-org/wago/codegen"
	rv "github.com/wago-org/wago/codegen/riscv64"
	"strings"
	"testing"
)

type wrongRV64Lowering codegen.Architecture

func (l wrongRV64Lowering) Architecture() codegen.Architecture { return codegen.Architecture(l) }
func TestRV64InstructionCodegenContracts(t *testing.T) {
	spec := InstructionSpec{Module: "example", Name: "identity", Input: []int32{32}, Output: []int32{32}, Handler: func(InstructionContext, []Bits) ([]Bits, error) { return nil, nil }}
	spec.Codegen = &rv.Lowering{Compatibility: rv.CompatibilityManaged, Managed: func(rv.ManagedContext) error { return nil }}
	def, err := Prepare(spec)
	if err != nil {
		t.Fatal(err)
	}
	native, ok := def.Native()
	if !ok || native.Codegen.Architecture() != codegen.RISCV64 {
		t.Fatal("RV64 lowering omitted", native, ok)
	}
	original := spec.Codegen.(*rv.Lowering)
	original.Managed = nil
	if got := native.Codegen.(*rv.Lowering); got.Managed == nil {
		t.Fatal("caller mutated prepared lowering")
	}
	for _, arch := range []codegen.Architecture{codegen.AMD64, codegen.ARM64, codegen.RISCV64} {
		spec.Codegen = wrongRV64Lowering(arch)
		if _, err := Prepare(spec); err == nil || !strings.Contains(err.Error(), "riscv64 build") {
			t.Fatal("wrong target admitted", arch, err)
		}
	}
	spec.Codegen = &rv.Lowering{Compatibility: rv.CompatibilityManaged, Managed: func(rv.ManagedContext) error { return nil }, Emit: func(rv.Context) error { return nil }}
	if _, err := Prepare(spec); err == nil {
		t.Fatal("two callback contracts admitted")
	}
	spec.Codegen = &rv.Lowering{Compatibility: rv.CompatibilityFullAccess, Emit: func(rv.Context) error { return nil }}
	if _, err := Prepare(spec); err != nil {
		t.Fatal(err)
	}
	spec.Input = []int32{64}
	if _, err := Prepare(spec); err == nil {
		t.Fatal("direct wide carrier admitted")
	}
	if cloneMachineCode(nil) != nil {
		t.Fatal("host-only codegen")
	}
}
