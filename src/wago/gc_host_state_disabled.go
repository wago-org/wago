// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_no_gc

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

type hostGCBridgeCapability struct{}

func (*HostFuncRef) allowsGCBridge() bool { return false }

func (*HostFuncRef) setGCBridgeCapability(bool) {}

type hostGCBridgeBinding struct{}

func (*hostFuncRefBindingState) gcDomain() (*gc.Collector, uint64) { return nil, 0 }

func (*hostFuncRefBindingState) bindGCCollector(*gc.Collector, uint64) {
	panic("GC host bridge binding is disabled in this build")
}

func (*HostFuncRef) isGCBridge() bool { return false }

type hostGCResultJournal struct{}

func (*hostGCResultJournal) gcResultTokens() *gcHostTempTokens { return nil }

func (*hostGCResultJournal) addGCResult(uint64) {
	panic("GC callback results are disabled in this build")
}

func (*hostGCResultJournal) releaseGCResults(*Instance) {}
