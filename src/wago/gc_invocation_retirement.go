// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// Cold, store-mutex-protected state. One driver (possibly the uncontended
// releasing caller) owns each batch through its final callbacks. Requests and
// the idle/exit transition share the mutex, so no wakeup can be lost.
type gcInvocationRetirement struct {
	pending  map[*gcStoreDomain]struct{}
	terminal bool
	running  bool
	idle     chan struct{}
}

func (s *referenceStore) queueInvocationRetirementLocked(domains []*gcStoreDomain, terminal bool) bool {
	if len(domains) == 0 && !terminal {
		return false
	}
	topology := s.gcDomains
	r := topology.retirement
	if r == nil {
		r = &gcInvocationRetirement{}
		topology.retirement = r
	}
	if len(domains) != 0 && r.pending == nil {
		r.pending = make(map[*gcStoreDomain]struct{})
	}
	for _, d := range domains {
		r.pending[d] = struct{}{}
	}
	r.terminal = r.terminal || terminal
	if r.running {
		return false
	}
	r.running = true
	r.idle = make(chan struct{})
	return true
}

// Acquire the writer or subscribe atomically to a gate transition. Unlike Lock,
// this never reserves a writer ticket: a waiting retirement must not prevent a
// new reader from entering to signal an existing reader's shared-memory wait.
// No other mutex is held, and the wait channel is not slept on by the caller.
func (topology *gcDomainTopology) tryInvocationRetirementWriter() (bool, <-chan struct{}) {
	topology.mu.Lock()
	defer topology.mu.Unlock()
	if !topology.writer && topology.readers == 0 && topology.writers == 0 {
		topology.writer = true
		return true, nil
	}
	if topology.changed == nil {
		topology.changed = make(chan struct{})
	}
	return false, topology.changed
}

func (s *referenceStore) driveInvocationRetirement() {
	topology := s.gcDomains // topology sidecar is stable for the store lifetime
	locked, wait := topology.tryInvocationRetirementWriter()
	if !locked {
		go s.runInvocationRetirement(wait)
		return
	}
	if s.finishInvocationRetirementBatch(topology) {
		go s.runInvocationRetirement(nil)
	}
}

// Once contended, a single goroutine handles all subsequent batches, including
// work enqueued by its own callbacks. No goroutine is created per domain/request.
func (s *referenceStore) runInvocationRetirement(wait <-chan struct{}) {
	topology := s.gcDomains
	for {
		if wait != nil {
			<-wait
		}
		locked, changed := topology.tryInvocationRetirementWriter()
		if !locked {
			wait = changed
			continue
		}
		if !s.finishInvocationRetirementBatch(topology) {
			return
		}
		wait = nil
	}
}

// The topology writer makes registration validation and unlink one operation.
// A candidate can be revived, or retired by another path, while queued: neither
// case permits this driver to close it. Only a successful unlink owns Close.
func (s *referenceStore) finishInvocationRetirementBatch(topology *gcDomainTopology) (more bool) {
	s.mu.Lock()
	r := topology.retirement
	var collectors []*gc.Collector
	for d := range r.pending {
		if topology.domainForCollectorLocked(d.collector) == d && d.refs == 0 && d.claims == 0 && topology.unlinkDomainLocked(d) {
			collectors = append(collectors, d.collector)
		}
	}
	clear(r.pending)
	terminal := r.terminal
	r.terminal = false
	var entries referenceTokenEntries
	if terminal {
		entries = s.maybeReleaseEntriesLocked()
	}
	s.mu.Unlock()
	topology.Unlock()
	// Account for requests made by finalizers, including requests crossing the
	// last callback/worker-exit boundary. On panic transfer queued work before
	// propagating; otherwise the existing driver continues without spawning.
	completed := false
	defer func() {
		s.mu.Lock()
		more = len(r.pending) != 0 || r.terminal
		if !more {
			r.running = false
			close(r.idle)
		}
		s.mu.Unlock()
		if !completed && more {
			go s.runInvocationRetirement(nil)
		}
	}()
	var cleanup resourceCleanup
	for _, c := range collectors {
		cleanup.run(c.Close)
	}
	cleanup.run(func() { releaseLifetimeEntries(nil, entries) })
	cleanup.finish()
	completed = true
	return
}
