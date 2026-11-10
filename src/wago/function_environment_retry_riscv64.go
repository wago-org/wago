// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

// RV64 retains its existing receipt and invocation-return scheduling contract.
type managedFunctionRetryState struct{}

func managedFunctionDomainRegisteredLocked(*referenceStore, *Instance, *gcStoreDomain) bool {
	return true
}

func (*managedFunctionGraph) queueHostRetry(managedFunctionRetryWait) {}

func tryManagedFunctionInvocation(g *invocationGate) bool {
	return g.state.CompareAndSwap(0, invocationGateHeld)
}
