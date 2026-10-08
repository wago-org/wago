//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

// Compiled cleanup owns only detached mappings and their charges. It never
// retains instance contexts or writes a native-mutex-protected entry ledger.
// Record/list storage is charged before each generation is constructed.
type rv64RetiredTailGeneration struct {
	factory        *railshot.NativeTailDispatcher
	code, metadata int
}

// The caller holds native serialization. All candidates remain detached until
// the complete graph is built and checked, so a failed growth attempt preserves
// existing bindings. Old code stays alive through parks and cold decoration.
func (s *rv64NativeTableTargets) refreshTailGenerations() error {
	if s.tailOwnerCount == s.count {
		if len(s.tailActive) == 0 {
			return nil
		}
		return rv64ActivateNativeTailGenerations(s.tailActive)
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
		scopePolicy := s.owners[0].ensurePluginState().railshotInvocation.tailPolicy
		callerPolicy := caller.ensurePluginState().railshotInvocation.tailPolicy
		if !scopePolicy.ready || !callerPolicy.ready {
			return rv64TailGenerationError("missing captured allocation policy")
		}
		g, err := rv64NewDefinedTailGenerationWithBudgets(caller, s, m.Plans, scopePolicy.limits, callerPolicy.limits)
		if g != nil {
			// This owns partial construction too. It is intentionally retained
			// on error until the entry's ordinary retirement path runs.
			s.tailGenerations = append(s.tailGenerations, g)
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
	s.tailActive = append(s.tailActive[:0], candidates[:n]...)
	s.tailOwnerCount = s.count
	return nil
}

// Detach/retire while native execution is still serialized. Mapping-only
// failures transfer into compiled cleanup; a changed capability slot retains
// this exact lease instead, so its arena cannot be freed or its slot reused.
func railshotRetireNativeTableTargets(lease railshotTableEntryLease) error {
	s := lease.targets
	if s == nil {
		return nil
	}
	s.tailRetiring = true
	var errs []error
	for _, g := range s.tailGenerations {
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
	clear(s.tailGenerations)
	clear(s.tailActive)
	// Their backing arrays were charged to the retired generations.
	s.tailGenerations, s.tailActive = nil, nil
	s.tailOwnerCount = 0
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
		state := &g.caller.c.ensureCompileIndexes().railshotTail
		state.retiredGenerations = append(state.retiredGenerations, rv64RetiredTailGeneration{g.factory, g.codeBytes, g.metadataBytes})
		state.retiredGenerationCode += g.codeBytes
		state.retiredGenerationMetadata += g.metadataBytes
		cc.mu.Unlock()
	}
	g.releaseReservation()
}

// A failed Detach quarantines the exact top lease. Only the root's invocation
// gate may retry it, before acquiring a fresh native entry. Physical release
// still happens after unlocking, including when the last root finalizes.
func railshotRetryNativeTableRetirement(root *Instance) error {
	state := root.pluginState.Load()
	if state == nil {
		return nil
	}
	pool := &state.railshotInvocation
	for pool.tableDepth != 0 {
		slot := pool.tableDepth - 1
		targets := pool.tableTargets[slot]
		if !targets.tailRetiring {
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
	// Physical finalizers can run while the final scoped roots drain. Preserve
	// Close's completion contract even when this runs inside its recovery defer.
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("wago: RV64 closed table retirement: %w", ErrCallbackPanic)
		}
	}()
	if root == nil || root.invocationState.Load() != instanceInvocationClosed {
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
	defer gate.Unlock()
	return railshotRetryNativeTableRetirement(root)
}
