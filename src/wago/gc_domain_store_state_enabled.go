// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_reference_store && !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

type gcDomainStoreState struct {
	topology *gcDomainTopology
}

func newGCDomainStoreState(topology *gcDomainTopology) gcDomainStoreState {
	return gcDomainStoreState{topology: topology}
}

func (s *referenceStore) gcTopologyLocked() *gcDomainTopology {
	return s.topology
}

func (s *referenceStore) ensureGCTopologyLocked() *gcDomainTopology {
	if s.topology == nil {
		s.topology = new(gcDomainTopology)
	}
	return s.topology
}

func (s *referenceStore) takeGCCollectorsLocked() []*gc.Collector {
	topology := s.topology
	if topology == nil {
		return nil
	}
	var collectors []*gc.Collector
	for domain := topology.first; domain != nil; domain = domain.next {
		collectors = append(collectors, domain.collector)
	}
	topology.first = nil
	topology.last = nil
	topology.n = 0
	topology.byCollector = nil
	topology.gcFrameCodeDirectory = gcFrameCodeDirectory{}
	// Table attachments can outlive logical closure and token/GC retirement.
	// Their physical owners consume these exact counts at final detach.
	return collectors
}
