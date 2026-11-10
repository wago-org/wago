// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build tinygo || (!amd64 && !arm64) || (!linux && !darwin && !windows)

package wago

import "fmt"

const preparedDirectPairSupported = false

func (in *Instance) invokeDirectIntPairEntry(uintptr, uint8, []bool, []uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer pair entry is unavailable")
}

func (fn *WasmFunc) invokeDirectIntPairSession(uint64, uint64, uint64, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer pair entry is unavailable")
}
