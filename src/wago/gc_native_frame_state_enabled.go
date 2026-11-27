// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

import coreruntime "github.com/wago-org/wago/src/core/runtime"

type gcNativeFrameRoots struct {
	owner                *Instance
	physicalStack        *coreruntime.Engine
	base                 uintptr
	offsets              []uint32
	frameBytes           uint32
	frameLayout          uint8
	allowExternalReturn  bool
	domainHostRoots      bool // bounded products also retain older same-domain host activations
	codeBase             uintptr
	codeBytes            uintptr
	adapterReturnOffsets []uint32
	callsites            []compiledGCFrameCallsite
	suspended            *gcPublicState
}

func gcCollectFrameRoots(in *Instance, public *gcPublicState, frameLayout uint8, allowExternalReturn bool) gcNativeFrameRoots {
	return gcNativeFrameRoots{
		owner:               in,
		frameLayout:         frameLayout,
		allowExternalReturn: allowExternalReturn,
		suspended:           public,
	}
}

func (r *gcNativeFrameRoots) clearPhysicalStack() {
	r.physicalStack = nil
}

//lint:ignore U1000 Used by the RISC-V managed-function collector.
func (r *gcNativeFrameRoots) omitDomainHostRoots() {
	r.domainHostRoots = false
}
