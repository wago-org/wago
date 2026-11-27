// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_no_gc || (!amd64 && !arm64)

package wago

type preparedGCScalarState struct{}

func (*WasmFunc) prepareGCScalar() {}

func (*WasmFunc) hasPreparedGCScalar() bool   { return false }
func (*WasmFunc) hasPrivateGCContext() bool   { return false }
func (*WasmFunc) hasRevocableGCContext() bool { return false }

func (*WasmFunc) tryInvokeRevocableGCScalar(uint64) ([]uint64, error, bool) {
	return nil, nil, false
}

func (fn *WasmFunc) invokeGCScalar(args []uint64) ([]uint64, error) {
	return fn.invokeScalar(args)
}

func (fn *WasmFunc) callGCPrepared() error { return fn.in.callNativeSync(fn.entry) }
