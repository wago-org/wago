// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone_no_collector || (!(linux && amd64 && !wago_no_gc) && !((linux || darwin) && arm64 && !wago_no_gc) && !(linux && riscv64 && !tinygo && !wago_target_tinygo && (!wago_no_gc || !wago_no_exception_handling)))

package wago

import coreruntime "github.com/wago-org/wago/src/core/runtime"

type gcHostSavedControl [0]uint64

func (*Instance) pushGCHostActivation(uintptr, uint32, *coreruntime.Engine) gcHostActivationToken {
	return gcHostActivationToken{}
}

func popGCHostActivation(gcHostActivationToken) {}
