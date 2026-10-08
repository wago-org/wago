//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import "testing"

func TestRV64DependencyPrototypePreservesAnonymousShadow(t *testing.T) {
	g := dependencyTestGraph(t, 2, 2)
	target := dependencyTestMember(t, g, false)
	consumer := dependencyTestMember(t, g, true)
	// A preexisting anonymous root is not available for receipt adoption.
	if !target.owner.retainResourceRoot() {
		t.Fatal("anonymous root acquisition failed")
	}
	defer target.owner.releaseResourceRoot()
	r := dependencyTestAcquire(t, g, target)
	if typed, err := r.adopt(consumer, dependencyFunction); !typed || err != nil {
		t.Fatalf("prototype adoption: %v %v", typed, err)
	}
	dependencyTestCounts(t, g, target, 2, 1, 0, 1)
	target.owner.lifeMu.Lock()
	ledger := target.owner.importState.Load().rootAccounting.ledger
	external := ledger.external
	target.owner.lifeMu.Unlock()
	if external != 2 {
		t.Fatal("prototype consumed anonymous shadow accounting")
	}
	if !r.release() {
		t.Fatal("typed prototype receipt did not release")
	}
	dependencyTestCounts(t, g, target, 1, 0, 0, 0)
	target.owner.lifeMu.Lock()
	retainedLedger := target.owner.importState.Load().rootAccounting.ledger
	external = retainedLedger.external
	target.owner.lifeMu.Unlock()
	if retainedLedger != ledger || external != 1 {
		t.Fatal("prototype release consumed or replaced unrelated anonymous root")
	}
}
