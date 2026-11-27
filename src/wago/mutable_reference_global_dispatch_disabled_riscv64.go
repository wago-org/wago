// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && wago_no_reference_types && wago_no_typed_function_references && wago_no_gc && wago_no_exception_handling

package wago

import "errors"

func railshotUsesGuestGlobalStoreHelper(*Compiled) bool { return false }

func (*Instance) retainGuestGlobalStoreDescriptor(uint32, []uint64, []uint64) (*Instance, error) {
	return nil, nil
}

func (in *Instance) dispatchGuestGlobalStore(dispatch uint32, args, results []uint64) bool {
	return in.dispatchGuestGlobalStoreWithOwner(dispatch, args, results, nil)
}

func (*Instance) dispatchGuestGlobalStoreWithOwner(dispatch uint32, _ []uint64, _ []uint64, _ *Instance) bool {
	if dispatch == hostFuncRefDispatchBit {
		panic(invalidHostReference{err: errors.New("invalid RV64 guest global store bridge")})
	}
	return false
}
