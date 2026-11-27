// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (!linux && !darwin && !windows) || (!amd64 && !arm64) || tinygo || wago_no_gc

package wago

type compiledGCInductionState struct{}
type preparedGCInductionState struct{}

func (*Compiled) setPreparedGCInductions([]uint64) {}
func (*WasmFunc) prepareGCInductions(int)          {}
func (*WasmFunc) hasPreparedI31Induction() bool    { return false }
func (*WasmFunc) hasPreparedDeadInduction() bool   { return false }

func (*WasmFunc) tryInvokePreparedI31(uint64) ([]uint64, error, bool) {
	return nil, nil, false
}

func (*WasmFunc) tryInvokePreparedDeadInduction(uint64, *instancePluginState) ([]uint64, error, bool) {
	return nil, nil, false
}
