// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (!amd64 && !arm64 && !(linux && riscv64 && !tinygo && !wago_target_tinygo)) || (tinygo && amd64 && !linux) || (arm64 && !(linux || darwin || windows)) || (tinygo && arm64 && windows)

package wago

import "fmt"

const preparedDirectIntSupported = false
const preparedDirectIntPrivateSupported = false
const preparedIntCallBlockDefault = false

func (fn *WasmFunc) initDirectIntCall() {}

func (in *Instance) invokeCachedDirectInt1(*invokeCache, uintptr, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (in *Instance) invokeCachedDirectI32ToI32(*invokeCache, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (fn *WasmFunc) invokeDirectInt([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (fn *WasmFunc) invokeDirectIntFixed(uint64, uint64, uint64, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (fn *WasmFunc) invokeDirectIntSession(uint64, uint64, uint64, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (in *Instance) invokeDirectIntEntry(uintptr, int, int, uint8, bool, bool, bool, bool, uint64, uint64, uint64, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}
