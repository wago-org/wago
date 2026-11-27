// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"

// Compiled cleanup owns only detached mappings and their charges. It never
// retains instance contexts or writes a native-mutex-protected entry ledger.
// Record/list storage is charged before each generation is constructed.
type rv64RetiredTailGeneration struct {
	factory        *railshot.NativeTailDispatcher
	code, metadata int
}

type rv64NativeTailTableState struct {
	budget      rv64NativeTailGenerationBudget
	generations []*rv64NativeTailGeneration // all versions, including suspended code
	active      []*rv64NativeTailGeneration
	ownerCount  int
	retiring    bool
}

// The caller holds native serialization. All candidates remain detached until
// the complete graph is built and checked, so a failed growth attempt preserves
// existing bindings. Old code stays alive through parks and cold decoration.
func (s *rv64NativeTableTargets) refreshTailGenerations() error {
	tail := s.tail.get()
	if tail.ownerCount == s.count {
		if len(tail.active) == 0 {
			return nil
		}
		return rv64ActivateNativeTailGenerations(tail.active)
	}
	var candidates [rv64MaxTailGenerations]*rv64NativeTailGeneration
	n := 0
	for _, caller := range s.owners[:s.count] {
		m := rv64NativeTailModule(caller.c)
		if m == nil || len(m.Plans) == 0 {
			continue
		}
		dynamic := false
		for _, p := range m.Plans {
			dynamic = dynamic || p.Kind != 0
		}
		if !dynamic {
			continue // Existing static direct factories retain their own protocol.
		}
		if n == len(candidates) {
			return rv64TailGenerationError("entry caller count")
		}
		// Retry detached failures before every replacement allocation, including
		// graph growth and another instance of the same compiled module.
		cc := caller.c.loadCodeCache()
		cc.mu.Lock()
		err := railshotRetryNativeTail(caller.c)
		cc.mu.Unlock()
		if err != nil {
			return err
		}
		scopePolicy := s.owners[0].ensurePluginState().railshotInvocation.tail.get().policy
		callerPolicy := caller.ensurePluginState().railshotInvocation.tail.get().policy
		if !scopePolicy.ready || !callerPolicy.ready {
			return rv64TailGenerationError("missing captured allocation policy")
		}
		g, err := rv64NewDefinedTailGenerationWithBudgets(caller, s, m.Plans, scopePolicy.limits, callerPolicy.limits)
		if g != nil {
			// This owns partial construction too. It is intentionally retained
			// on error until the entry's ordinary retirement path runs.
			tail.generations = append(tail.generations, g)
		}
		if err != nil {
			return rv64TailPolicyError(err, scopePolicy, callerPolicy)
		}
		candidates[n] = g
		n++
	}
	if n != 0 {
		if err := rv64ActivateNativeTailGenerations(candidates[:n]); err != nil {
			return err
		}
	}
	tail.active = append(tail.active[:0], candidates[:n]...)
	tail.ownerCount = s.count
	return nil
}

func (g *rv64NativeTailGeneration) retire() error {
	g.ready = false
	if err := g.deactivate(); err != nil {
		return err
	}
	// Establish mapping-only cleanup before allowing any ownership transfer.
	if err := g.factory.Detach(); err != nil {
		return err
	}
	g.finishDetachedRetirement(g.factory.Close())
	return nil
}

// closeErr comes from Close after successful Detach. This handoff is separate
// so failure-ledger tests can exercise mapping errors without leaking mappings
// or replacing process-global runtime syscall hooks.
func (g *rv64NativeTailGeneration) finishDetachedRetirement(closeErr error) {
	if closeErr != nil {
		cc := g.caller.c.loadCodeCache()
		cc.mu.Lock()
		state := g.caller.c.ensureCompileIndexes().railshotTail.tailCode.get()
		state.retiredGenerations = append(state.retiredGenerations, rv64RetiredTailGeneration{g.factory, g.codeBytes, g.metadataBytes})
		state.retiredGenerationCode += g.codeBytes
		state.retiredGenerationMetadata += g.metadataBytes
		cc.mu.Unlock()
	}
	g.releaseReservation()
}
