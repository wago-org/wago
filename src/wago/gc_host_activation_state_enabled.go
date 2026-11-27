// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

import coreruntime "github.com/wago-org/wago/src/core/runtime"

type gcHostActivation struct {
	base         uintptr
	ctrl         uintptr
	stack        *coreruntime.Engine
	callsite     uint32
	noFrame      bool
	savedControl gcHostSavedControl
}

type gcHostActivationState struct {
	hostActivations     [gcHostActivationLimit]gcHostActivation
	hostActivationCount uint8
	hostRootPlan        *compiledGCFrameRoots
	hostCodeBase        uintptr
	hostCodeBytes       uintptr
}

func (s *gcHostActivationState) hasGCHostActivations() bool {
	return s.hostActivationCount != 0
}

func (s *gcHostActivationState) hasGCHostActivation(index uint8) bool {
	return index < s.hostActivationCount && s.hostActivations[index].stack != nil
}
