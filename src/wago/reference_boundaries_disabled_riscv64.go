// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && wago_no_reference_types && wago_no_typed_function_references && wago_no_gc && wago_no_exception_handling

package wago

import encoder "github.com/wago-org/wago/src/core/encoder/riscv64"

func rv64PublicReferenceCallBoundary(*Compiled) bool                         { return false }
func rv64PublicHostReferenceBoundary(*Compiled) bool                         { return false }
func rv64PublicDynamicHostReferenceBoundary(*Compiled) bool                  { return false }
func rv64PublicReferenceCallBoundaryWithPlan(*Compiled, bool) bool           { return false }
func rv64PublicHostReferenceBoundaryWithPlan(*Compiled, bool) bool           { return false }
func rv64PublicDynamicHostReferenceBoundaryWithPlan(*Compiled, bool) bool    { return false }
func rv64PublicHostReferenceTailPlan(*Compiled, encoder.NativeTailPlan) bool { return false }
func rv64PublicDynamicHostReferenceTailPlan(*Compiled, encoder.NativeTailPlan, uint32) bool {
	return false
}
