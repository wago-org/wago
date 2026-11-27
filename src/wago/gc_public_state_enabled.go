// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_reference_store && !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

import "sync/atomic"

type pluginPublicGCState struct {
	gcPublic atomic.Pointer[gcPublicState]
}

func (in *Instance) publicGCState() *gcPublicState {
	plugin := in.ensurePluginState()
	state := plugin.gcPublic.Load()
	if state == nil {
		candidate := &gcPublicState{}
		if plugin.gcPublic.CompareAndSwap(nil, candidate) {
			state = candidate
		} else {
			state = plugin.gcPublic.Load()
		}
	}
	return state
}

func (in *Instance) existingPublicGCState() *gcPublicState {
	if in == nil {
		return nil
	}
	plugin := in.pluginState.Load()
	if plugin == nil {
		return nil
	}
	return plugin.gcPublic.Load()
}

type gcHostActivationToken struct {
	state *gcPublicState
	index uint8
}

func newGCHostActivationToken(state *gcPublicState, index uint8) gcHostActivationToken {
	return gcHostActivationToken{state: state, index: index}
}

func gcHostActivationRootState(t gcHostActivationToken) (*gcPublicState, uint8) {
	return t.state, t.index
}
