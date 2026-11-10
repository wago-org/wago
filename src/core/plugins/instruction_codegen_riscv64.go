// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build riscv64

package plugins

import (
	"fmt"
	"github.com/wago-org/wago/codegen"
	rvcodegen "github.com/wago-org/wago/codegen/riscv64"
)

func prepareMachineCode(spec InstructionSpec) (codegen.Lowering, error) {
	if spec.Codegen == nil {
		return nil, nil
	}
	lowering, ok := spec.Codegen.(*rvcodegen.Lowering)
	if !ok || lowering == nil {
		return nil, fmt.Errorf("wago: instruction %q.%q has %s codegen in a riscv64 build", spec.Module, spec.Name, spec.Codegen.Architecture())
	}
	switch lowering.Compatibility {
	case rvcodegen.CompatibilityManaged:
		if lowering.Managed == nil || lowering.Emit != nil {
			return nil, fmt.Errorf("wago: instruction %q.%q managed riscv64 lowering requires Managed and forbids Emit", spec.Module, spec.Name)
		}
	case rvcodegen.CompatibilityFullAccess:
		if lowering.Emit == nil || lowering.Managed != nil {
			return nil, fmt.Errorf("wago: instruction %q.%q full-access riscv64 lowering requires Emit and forbids Managed", spec.Module, spec.Name)
		}
	default:
		return nil, fmt.Errorf("wago: instruction %q.%q requires an explicit riscv64 compatibility mode", spec.Module, spec.Name)
	}
	if err := validateMachineCodeWidths(spec, "riscv64"); err != nil {
		return nil, err
	}
	copy := *lowering
	return &copy, nil
}
func cloneMachineCode(lowering codegen.Lowering) codegen.Lowering {
	if lowering == nil {
		return nil
	}
	copy := *(lowering.(*rvcodegen.Lowering))
	return &copy
}
