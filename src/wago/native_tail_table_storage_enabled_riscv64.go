// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_tail_call

package wago

import (
	"errors"
	"fmt"
)

type rv64NativeTailTableStorage rv64NativeTailTableState

func (s *rv64NativeTailTableStorage) get() *rv64NativeTailTableState {
	return (*rv64NativeTailTableState)(s)
}

// Detach/retire while native execution is still serialized. Mapping-only
// failures transfer into compiled cleanup; a changed capability slot retains
// this exact lease instead, so its arena cannot be freed or its slot reused.
func railshotRetireNativeTableTargets(lease railshotTableEntryLease) error {
	s := lease.targets
	if !dynamicNativeTailForImage || s == nil {
		return nil
	}
	tail := s.tail.get()
	tail.retiring = true
	var errs []error
	for _, g := range tail.generations {
		if g == nil || !g.reserved {
			continue
		}
		if err := g.retire(); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	clear(tail.generations)
	clear(tail.active)
	// Their backing arrays were charged to the retired generations.
	tail.generations, tail.active = nil, nil
	tail.ownerCount = 0
	return nil
}

// A failed Detach quarantines the exact top lease. Only the root's invocation
// gate may retry it, before acquiring a fresh native entry. Physical release
// still happens after unlocking, including when the last root finalizes.
func railshotRetryNativeTableRetirement(root *Instance) error {
	if !dynamicNativeTailForImage {
		return nil
	}
	state := root.pluginState.Load()
	if state == nil {
		return nil
	}
	pool := &state.railshotInvocation
	for pool.tableDepth != 0 {
		slot := pool.tableDepth - 1
		targets := pool.tableTargets[slot]
		if !targets.tail.get().retiring {
			break
		}
		locked, err := root.lockAndBindNativeEntry()
		if err != nil {
			return err
		}
		lease := railshotTableEntryLease{root: root, targets: targets, slot: slot}
		err = railshotRetireNativeTableTargets(lease)
		locked.unlockExecution() // no table lease on this temporary lock
		if err != nil {
			return err
		}
		railshotReleaseNativeTableTargets(lease)
	}
	return nil
}

// A logically closed, quiescent root cannot admit another public entry. Repeated
// Close can nevertheless retry its quarantined lease without calling guest
// code. The invocation gate serializes competing close retries; the closed bit
// and zero count exclude active or parked root invocations without waiting for
// callbacks. Physical root release may finalize the instance during the retry.
func railshotRetryClosedNativeTableRetirement(root *Instance) (err error) {
	if !dynamicNativeTailForImage || root == nil || root.invocationState.Load() != instanceInvocationClosed {
		return nil
	}
	state := root.pluginState.Load()
	if state == nil {
		return nil
	}
	// Close callbacks and concurrent closes join promptly rather than waiting
	// on the cleanup that may itself be releasing their final physical root.
	gate := &state.invokeMu
	if !gate.state.CompareAndSwap(0, invocationGateHeld) && !gate.state.CompareAndSwap(invocationGateRevoked, invocationGateRevoked|invocationGateHeld) {
		return nil
	}
	// Physical finalizers can run while the final scoped roots drain. Journal
	// gate release after retirement so Close completes and reports even a legacy
	// panic(nil), including when this retry runs inside Close's recovery defer.
	var cleanup resourceCleanup
	cleanup.run(func() { err = railshotRetryNativeTableRetirement(root) })
	cleanup.run(gate.Unlock)
	if cleanup.panicked {
		return fmt.Errorf("wago: RV64 closed table retirement: %w", ErrCallbackPanic)
	}
	return err
}
