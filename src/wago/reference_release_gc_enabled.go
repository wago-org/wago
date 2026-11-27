// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

type gcReleaseBatch struct {
	collectors []*gc.Collector
}

func (batch *gcReleaseBatch) claimGCCollectorsLocked(store *referenceStore) {
	if collectorRuntimeForImage {
		batch.collectors = store.takeGCCollectorsLocked()
	}
}

func (batch *gcReleaseBatch) releaseGCCollectors(cleanup *resourceCleanup, closeCollector func(*gc.Collector)) {
	for i, collector := range batch.collectors {
		batch.collectors[i] = nil
		cleanup.run(func() { closeCollector(collector) })
	}
}

func (s *referenceStore) releaseUnclaimedGCCollector(collector *gc.Collector) {
	if s == nil || collector == nil {
		return
	}
	s.mu.Lock()
	topology := s.gcTopologyLocked()
	if topology == nil {
		s.mu.Unlock()
		return
	}
	// A failed construction can drop its claim without waiting for live readers.
	domain := topology.domainForCollectorLocked(collector)
	if domain != nil {
		if domain.claims > 0 {
			domain.claims--
		}
		if domain.refs != 0 || domain.claims != 0 {
			s.mu.Unlock()
			return
		}
	}
	s.mu.Unlock()
	if domain == nil {
		return
	}
	topology.gate.Lock()
	defer topology.gate.Unlock()
	s.mu.Lock()
	domain = topology.domainForCollectorLocked(collector)
	if domain == nil || domain.refs != 0 || domain.claims != 0 || !topology.unlinkDomainLocked(domain) {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	collector.Close()
}

// releaseLifetimeEntries receives already-claimed cleanup units after the
// store and topology locks have been dropped. A collector callback panic must
// not strand the separate token batch removed from the store maps.
func releaseLifetimeEntries(collector *gc.Collector, entries referenceTokenEntries) {
	if !collectorRuntimeForImage {
		releaseReferenceEntries(entries)
		return
	}
	releaseLifetimeEntriesWith(collector, entries, (*gc.Collector).Close)
}

func releaseLifetimeEntriesWith(collector *gc.Collector, entries referenceTokenEntries, closeCollector func(*gc.Collector)) {
	var cleanup resourceCleanup
	if collector != nil {
		cleanup.run(func() { closeCollector(collector) })
	}
	cleanup.run(func() {
		releaseReferenceEntriesWith(entries, releaseFuncrefTokenMetadata, gcResultRootMetadataReleaser(), closeCollector)
	})
	cleanup.finish()
}

func releaseReferenceEntries(entries referenceTokenEntries) {
	var closeCollector func(*gc.Collector)
	if collectorRuntimeForImage {
		closeCollector = (*gc.Collector).Close
	}
	releaseReferenceEntriesWith(entries, releaseFuncrefTokenMetadata, gcResultRootMetadataReleaser(), closeCollector)
}
