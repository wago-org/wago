// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone_no_collector || !linux || !riscv64 || tinygo || wago_target_tinygo || (wago_no_gc && wago_no_exception_handling)

package wago

const managedFunctionLifetimeBuilt = false

func managedFunctionRegisteredLocked(*referenceStore, *Instance)     {}
func managedFunctionLifetimeLocked(*referenceStore, *Instance, bool) {}
func managedFunctionPrepareRegistration(*referenceStore, *Instance)  {}

//lint:ignore U1000 Used by the RISC-V root ledger with the collector disabled.
func managedFunctionRootEpochLocked(*Instance) {}

//lint:ignore U1000 Used by the RISC-V root ledger with the collector disabled.
func managedFunctionRetainAllowedLocked(*Instance) bool { return true }

func managedFunctionBindInvocationState(*Instance, *instancePluginState)      {}
func managedFunctionAfterInvocationUnlock(*instancePluginState, invocationID) {}
func managedFunctionAfterClose(*Instance)                                     {}
func managedFunctionDeferFinalization(*Instance) bool                         { return false }
func drainManagedFunctionStoreReclaims(*referenceStore)                       {}
