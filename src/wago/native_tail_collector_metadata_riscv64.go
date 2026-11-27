// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_gc

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// This is a metadata grammar check, not execution permission. Encoding the
// execution receipt validates tail metadata too, so it cannot call the full
// receipt verifier recursively. Publication separately requires FreshRootedGC.
func rv64CollectorTailMetadata(c *Compiled) bool {
	return c != nil && c.nativeGCABIRequirement() == gc.NativeABIVersion && c.genericGCFrameRoots() != nil && c.requiredFeatures.IsEnabled(CoreFeatureGC)
}

func rv64CollectorTailDeclaredCarrier(c *Compiled, value ValueTypeDescriptor) bool {
	if value.Kind != ValueTypeReference {
		return false
	}
	abi, ok := value.ABIType(c.Types)
	return ok && isGCRefValType(abi) && !value.Ref.Exact
}
