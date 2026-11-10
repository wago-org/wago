// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import "fmt"

// The persisted call/control witnesses are separate from general typed-
// reference permission. Both operate on retained internal function references;
// only calls require declared-type transport arenas. Reference-valued external
// signatures and non-function descriptors remain outside this bounded profile.
func railshotValidateReferenceCalls(c *Compiled) error {
	if !c.nativeReferenceCallABI && !c.nativeReferenceControlABI {
		return nil
	}
	if !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || len(c.Funcs) == 0 {
		return fmt.Errorf("RV64 typed-reference descriptor/native ABI contract")
	}
	if (c.needsFuncRefDescs() || len(c.FuncTypeID) != 0) && len(c.FuncTypeID) != c.NumImports+len(c.Funcs) {
		return fmt.Errorf("RV64 typed-reference indexed descriptor extent")
	}
	return rv64ValidateDeclaredControlTypes(c)
}
