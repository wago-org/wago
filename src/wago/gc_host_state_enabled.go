// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_no_gc

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

type hostGCBridgeCapability struct {
	capable bool
}

func (h *HostFuncRef) allowsGCBridge() bool { return h.hostGCBridgeCapability.capable }

func (h *HostFuncRef) setGCBridgeCapability(capable bool) {
	h.hostGCBridgeCapability.capable = capable
}

type hostGCBridgeBinding struct {
	collector *gc.Collector
	domainID  uint64
}

func (b *hostFuncRefBindingState) gcDomain() (*gc.Collector, uint64) {
	if b == nil {
		return nil, 0
	}
	return b.collector, b.domainID
}

func (b *hostFuncRefBindingState) bindGCCollector(collector *gc.Collector, domainID uint64) {
	b.hostGCBridgeBinding = hostGCBridgeBinding{collector: collector, domainID: domainID}
}

func (h *HostFuncRef) isGCBridge() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	collector, domainID := h.binding.gcDomain()
	ok := h.allowsGCBridge() && !h.closed && collector != nil && domainID != 0
	h.mu.Unlock()
	return ok
}

type hostGCResultJournal struct {
	results *gcHostTempTokens
}

func (j *hostGCResultJournal) gcResultTokens() *gcHostTempTokens { return j.results }

func (j *hostGCResultJournal) addGCResult(token uint64) {
	if j.results == nil {
		j.results = &gcHostTempTokens{}
	}
	j.results.add(token)
}

func (j *hostGCResultJournal) releaseGCResults(in *Instance) {
	j.results.release(in)
}
