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
