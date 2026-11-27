// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import "sync"

const terminalDependencyCollectionSupported = true

const terminalDependencyCollectionBuilt = crossInstanceDependenciesForImage

// terminalCloseCollection assigns one owner across the logical-result and
// terminal-readiness crossing. Both parties publish under the same lock.
type terminalCloseCollection struct {
	terminalCollectionMu       sync.Mutex
	terminalCollectionClaimed  bool
	terminalCollectionDeferred bool
}

func (in *Instance) finishLogicalClose(state *instanceCloseState, err error) error {
	if !terminalDependencyCollectionBuilt {
		state.complete(err)
		return err
	}
	collectionEnabled := railshotTerminalClosedDependencyCollectionEnabled(in, state)
	var collect bool
	err, collect = state.finishLogicalCloseCollectionHandoff(err, collectionEnabled, func() error {
		return railshotClosedContainerAdoptionResult(in, state)
	})
	if collect {
		err = joinPrimary(err, callShutdownSafely("collect closed dependency component", func() {
			railshotCollectClosingDependencyComponent(in, state)
		}))
		// The terminal-first handoff leaves ordinary finalization to this
		// result owner. Keep its scoped permit and error journal through the
		// scan, then retry after the transient finalizing claim is clear.
		err = joinPrimary(err, callShutdownSafely("finalize closing dependency owner", func() {
			in.referenceLifetime().finalize()
		}))
		err = joinPrimary(err, callShutdownSafely("retry closing dependency component", func() {
			railshotCollectClosingDependencyComponent(in, state)
		}))
		state.completeLogicalCloseCollection(err)
	}
	railshotCompleteClosingDependencyCollection(in, state)
	return err
}

// finishLogicalCloseCollectionHandoff atomically chooses which side of the
// logical-result/terminal-readiness crossing owns the one collection attempt.
// If terminal work is not ready, publishing completed under the same lock
// guarantees that the terminal owner will observe it. If terminal work is
// ready, this Close owner claims collection and delays result publication until
// its callbacks have been accounted for.
func (state *instanceCloseState) finishLogicalCloseCollectionHandoff(result error, enabled bool, terminalResult func() error) (error, bool) {
	state.terminalCollectionMu.Lock()
	defer state.terminalCollectionMu.Unlock()
	ready := state.terminalComplete.Load()
	if ready && terminalResult != nil {
		result = joinPrimary(result, terminalResult())
	}
	required := enabled || state.terminalCollectionDeferred
	if required && ready && !state.terminalCollectionClaimed {
		state.terminalCollectionClaimed = true
		state.terminalCollectionDeferred = false
		return result, true
	}
	state.complete(result)
	return result, false
}

func (state *instanceCloseState) completeLogicalCloseCollection(result error) {
	state.terminalCollectionMu.Lock()
	state.complete(result)
	state.terminalCollectionMu.Unlock()
}

// publishTerminalCollectionReady is the other half of the handoff. Publishing
// readiness and observing completed share one mutex with the Close owner, so
// the crossing cannot leave both sides believing the other will collect.
func (state *instanceCloseState) publishTerminalCollectionReady() bool {
	state.terminalCollectionMu.Lock()
	defer state.terminalCollectionMu.Unlock()
	state.terminalComplete.Store(true)
	if !state.completed.Load() || state.terminalCollectionClaimed {
		if !state.completed.Load() {
			state.terminalCollectionDeferred = true
		}
		return false
	}
	state.terminalCollectionClaimed = true
	return true
}
