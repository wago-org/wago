// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone_no_collector || (wago_no_gc && (!linux || !riscv64 || tinygo || wago_target_tinygo || wago_no_exception_handling))

package wago

type gcNativeFrameRoots struct {
	owner *Instance
}

func gcCollectFrameRoots(in *Instance, _ *gcPublicState, _ uint8, _ bool) gcNativeFrameRoots {
	return gcNativeFrameRoots{owner: in}
}

func (*gcNativeFrameRoots) clearPhysicalStack() {}

func (*gcNativeFrameRoots) omitDomainHostRoots() {}
