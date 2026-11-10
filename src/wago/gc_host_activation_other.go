// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !(linux && amd64) && !((linux || darwin) && arm64) && !(linux && riscv64 && !tinygo && !wago_target_tinygo)

package wago

import coreruntime "github.com/wago-org/wago/src/core/runtime"

type gcHostSavedControl [0]uint64

// Keep the token shape uniform across platforms so shared root-management code
// compiles unchanged. Unsupported platforms always return the zero token.
type gcHostActivationToken struct {
	state *gcPublicState
	index uint8
}

func (*Instance) pushGCHostActivation(uintptr, uint32, *coreruntime.Engine) gcHostActivationToken {
	return gcHostActivationToken{}
}

func (*Instance) popGCHostActivation(gcHostActivationToken) {}
