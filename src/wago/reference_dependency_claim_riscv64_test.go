//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import "testing"

func TestRV64DependencyComponentClaimBlocksAnonymousFinalizationRetain(t *testing.T) {
	g := dependencyTestGraph(t, 1, 1)
	in := &Instance{}
	member := &rv64FunctionDependencyOwnershipFor(in).member
	if err := g.register(member); err != nil {
		t.Fatal(err)
	}
	dependencyTestInvocationLease(member)
	receipt := dependencyTestAcquire(t, g, member)
	if typed, err := receipt.adopt(member, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	dependencyTestClosedForDiscovery(t, member)
	// This is a claim-rollback fixture, not a lifecycle-trigger fixture. Keep
	// tryFinalize from manufacturing a second terminal owner when the synthetic
	// guard is released.
	in.closeState.Load().terminalStarted.Store(true)
	candidate := g.discoverClosedComponent(member, 1, 1)
	ledger := assertRootLedgerRV64(t, in, 1)
	claim, ok := g.claimClosedComponent(candidate)
	if !ok {
		t.Fatal("component claim failed")
	}
	t.Cleanup(func() { claim.release() })
	if in.resourceRefs != 2 || ledger.external != 2 {
		t.Fatal("component guard did not enter the RV64 root ledger")
	}
	if in.retainResourceRootForFinalization() {
		t.Fatal("anonymous finalization retain crossed component claim")
	}
	if !claim.release() || in.resourceRefs != 1 || ledger.external != 1 || !in.retainResourceRootForFinalization() {
		t.Fatal("claim rollback did not restore finalization retain")
	}
	if in.resourceRefs != 2 || ledger.external != 2 {
		t.Fatal("restored finalization retain bypassed the RV64 root ledger")
	}
	in.releaseResourceRoot()
	dependencyTestCounts(t, g, member, 1, 1, 0, 1)
}

func TestRV64DependencyComponentCollectionPermitIsExactAndRevalidated(t *testing.T) {
	g := dependencyTestGraph(t, 2, 2)
	aOwner, bOwner := &Instance{}, &Instance{}
	aOwnership := rv64FunctionDependencyOwnershipFor(aOwner)
	bOwnership := rv64FunctionDependencyOwnershipFor(bOwner)
	a, b := &aOwnership.member, &bOwnership.member
	if err := g.register(a); err != nil {
		t.Fatal(err)
	}
	if err := g.register(b); err != nil {
		t.Fatal(err)
	}
	dependencyTestInvocationLease(a, b)
	aToB := dependencyTestAcquire(t, g, b)
	bToA := dependencyTestAcquire(t, g, a)
	if typed, err := aToB.adopt(a, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	if typed, err := bToA.adopt(b, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	dependencyTestClosedForDiscovery(t, a, b)
	aState, bState := aOwner.closeState.Load(), bOwner.closeState.Load()
	aState.completed.Store(false)
	bState.completed.Store(false)
	aPermit := &dependencyComponentCollectionPermit{owner: aOwner, state: aState, member: a, graph: g, memberIdentity: a.identity}
	bPermit := &dependencyComponentCollectionPermit{owner: bOwner, state: bState, member: b, graph: g, memberIdentity: b.identity}
	prepare := func(ownership *rv64FunctionDependencyOwnership, state *instanceCloseState, permit *dependencyComponentCollectionPermit) {
		ownership.closedAdoptionMu.Lock()
		ownership.closedAdoptionState = state
		ownership.closedAdoptionReady = true
		ownership.closedCollectionPrepared = state
		ownership.closedCollectionPermit = permit
		ownership.closedAdoptionMu.Unlock()
	}
	clear := func(ownership *rv64FunctionDependencyOwnership) {
		ownership.closedAdoptionMu.Lock()
		ownership.closedCollectionPrepared = nil
		ownership.closedAdoptionMu.Unlock()
	}
	prepare(aOwnership, aState, aPermit)
	if candidate := g.discoverClosedComponent(a, 2, 2); candidate.status != dependencyComponentBlocked || len(candidate.members) != 0 {
		t.Fatal("ordinary collector used a prepared Close owner")
	}
	if candidate := g.discoverClosedComponentWithPermit(a, 2, 2, aPermit); candidate.status != dependencyComponentBlocked || len(candidate.members) != 0 {
		t.Fatal("permit admitted an unprepared component member")
	}
	prepare(bOwnership, bState, bPermit)
	wrong := &dependencyComponentCollectionPermit{owner: aOwner, state: bState, member: a, graph: g, memberIdentity: a.identity}
	if candidate := g.discoverClosedComponentWithPermit(a, 2, 2, wrong); candidate.status != dependencyComponentBlocked || len(candidate.members) != 0 {
		t.Fatal("foreign close-state permit admitted a component")
	}
	if candidate := g.discoverClosedComponentWithPermit(a, 2, 2, bPermit); candidate.status != dependencyComponentBlocked || len(candidate.members) != 0 {
		t.Fatal("valid permit for another seed admitted this component")
	}
	candidate := g.discoverClosedComponentWithPermit(a, 2, 2, aPermit)
	if candidate.status != dependencyComponentCandidate || len(candidate.members) != 2 {
		t.Fatal("exact permit did not discover the prepared component")
	}
	if release, ok := g.claimClosedComponentForPhysicalRelease(candidate); ok || release != nil {
		t.Fatal("ordinary physical claimant used prepared Close authority")
	}
	clear(bOwnership)
	if release, ok := g.claimClosedComponentForPhysicalReleaseWithPermit(candidate, aPermit); ok || release != nil {
		t.Fatal("physical claim did not revalidate every prepared member")
	}
	prepare(bOwnership, bState, bPermit)
	release, ok := g.claimClosedComponentForPhysicalReleaseWithPermit(candidate, aPermit)
	if !ok || release == nil {
		t.Fatal("exact prepared component claim failed")
	}
	for i, receipt := range []dependencyReceipt{aToB, bToA} {
		index, current := i, receipt
		if !release.runPhysicalJournal(index, func(cleanup *resourceCleanup) bool {
			cleanup.run(func() {
				if !current.release() {
					panic("prepared component receipt was not released")
				}
			})
			return true
		}) {
			t.Fatal("prepared component physical journal failed", i)
		}
	}
	if !release.complete() || release.claim.active.Load() || a.collecting.Load() || b.collecting.Load() {
		t.Fatal("prepared component claim did not complete")
	}
}
