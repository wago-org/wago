// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo && (wago_no_mutable_global || (wago_no_reference_types && wago_no_typed_function_references && wago_no_gc && wago_no_exception_handling))

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func admitMutableFunctionGlobalLocalCallWire([]byte) bool           { return false }
func admitStateOnlyMutableFunctionGlobalImport(*wasm.Module) bool   { return false }
func admitMutableFunctionGlobalGet(*wasm.Module) bool               { return false }
func admitMutableFunctionGlobalClear(*wasm.Module) bool             { return false }
func admitMutableFunctionGlobalSetSelf(*wasm.Module) bool           { return false }
func admitMutableFunctionGlobalCopy(*wasm.Module) bool              { return false }
func admitMutableFunctionGlobalMove(*wasm.Module) bool              { return false }
func admitMutableFunctionGlobalBidirectionalCopy(*wasm.Module) bool { return false }
func admitMutableFunctionGlobalLocalCopy(*wasm.Module) bool         { return false }
func admitMutableFunctionGlobalLocalPublish(*wasm.Module) bool      { return false }
func admitMutableFunctionGlobalLocalTransfer(*wasm.Module) bool     { return false }
func admitMutableFunctionGlobalLocalExport(*wasm.Module) bool       { return false }
func admitMutableFunctionGlobalLocalObserve(*wasm.Module) bool      { return false }
func admitMutableFunctionGlobalLocalCall(*wasm.Module) bool         { return false }

func admitMutableFunctionGlobalReferenceCarrierCall(*wasm.Module) bool { return false }
