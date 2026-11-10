// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build tinygo || (!amd64 && !arm64) || (!linux && !darwin && !windows)

package wago

import "fmt"

const preparedDirectFloatSupported = false

func (fn *WasmFunc) invokeDirectFloat([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared float entry is unavailable on this architecture")
}

func (fn *WasmFunc) invokeDirectFloatSession([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared float entry is unavailable on this architecture")
}

func (in *Instance) invokeDirectFloatEntry(uintptr, []bool, []bool, []uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared float entry is unavailable on this architecture")
}
