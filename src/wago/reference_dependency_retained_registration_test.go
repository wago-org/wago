//go:build linux && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"testing"
)

func TestDependencyRetainedRegistrationPhysicalGuard(t *testing.T) {
	for _, mode := range []string{"logical-close", "invocation-close", "finalizing", "no-root", "physical-release"} {
		t.Run(mode, func(t *testing.T) {
			// Unmapped owners isolate the actual root/registration operations. The
			// separate RV64 witness qualifies mapped producer teardown and native reentry.
			owner := &Instance{c: &Compiled{}}
			held := mode != "no-root"
			if held && !owner.retainResourceRoot() {
				t.Fatal("actual bootstrap retention")
			}
			owner.closed = true
			owner.invocationState.Store(instanceInvocationClosed)
			if mode == "invocation-close" {
				owner.closed = false
			}
			if mode == "finalizing" {
				owner.finalizing = true
			}
			if mode == "physical-release" {
				owner.resourcesClosed = true
			} // explicit private false-claim fault
			graph, err := newDependencyGraph(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			member := &dependencyMember{owner: owner}
			if err := graph.register(member); !errors.Is(err, errDependencyClosed) {
				t.Fatal("ordinary registration changed its closed-owner admission", err)
			}
			err = graph.registerRetained(member)
			denied := mode == "no-root" || mode == "physical-release"
			if denied {
				if !errors.Is(err, errDependencyClosed) || len(graph.owners) != 0 || member.graph.Load() != nil || member.identity != 0 || graph.nextIdentity != 1 {
					t.Fatal("missing physical guard acquired registry authority", err)
				}
			} else {
				if err != nil || graph.owners[owner] != member || owner.resourceRefs != 1 || member.identity != 1 || !member.active.Load() {
					t.Fatal("actual retained logical closure failed registration", err)
				}
				identity := member.identity
				if err := graph.registerRetained(member); err != nil || member.identity != identity || owner.resourceRefs != 1 || graph.nextIdentity != 2 {
					t.Fatal("active retained reuse changed identity or acquired another root", err)
				}
				owner.resourceRefs = 0 // private loss-of-guard fault; no concurrent native user
				if err := graph.registerRetained(member); !errors.Is(err, errDependencyClosed) || member.identity != identity || !member.active.Load() {
					t.Fatal("active-member reuse bypassed physical guard", err)
				}
				owner.resourceRefs = 1
				owner.resourcesClosed = true // even a positive corrupted count cannot reopen storage
				if err := graph.registerRetained(member); !errors.Is(err, errDependencyClosed) {
					t.Fatal("active member reused physically released storage", err)
				}
				owner.resourcesClosed = false
			}
			owner.closed = true
			owner.finalizing = false
			if held {
				owner.releaseResourceRoot()
			}
			if !denied {
				if err := graph.unregister(member); err != nil {
					t.Fatal("closed physical member did not retire", err)
				}
				if err := graph.registerRetained(member); err == nil || len(graph.owners) != 0 {
					t.Fatal("retained mode reopened a retired identity", err)
				}
			}
		})
	}
}

func TestDependencyRetainedRegistrationAuthorityAndCapacity(t *testing.T) {
	callbacks := 0
	owner := cleanupTestRootedProducer(t, func() { callbacks++ })
	graph, err := newDependencyGraph(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	member := &dependencyMember{owner: owner}
	if err := graph.registerRetained(member); err != nil {
		t.Fatal(err)
	}
	other, _ := newDependencyGraph(1, 1)
	if err := other.registerRetained(member); !errors.Is(err, errDependencyMember) || len(other.owners) != 0 || member.graph.Load() != graph || owner.resourceRefs != 1 {
		t.Fatal("foreign graph acquired the retained member", err)
	}
	impostor := &dependencyMember{owner: owner}
	if err := graph.registerRetained(impostor); !errors.Is(err, errDependencyMember) || impostor.graph.Load() != nil || impostor.identity != 0 {
		t.Fatal("second member took the producer's authority", err)
	}
	next := cleanupTestRootedProducer(t, func() { callbacks++ })
	nextMember := &dependencyMember{owner: next}
	if err := graph.registerRetained(nextMember); !errors.Is(err, errDependencyCapacity) || nextMember.graph.Load() != nil || nextMember.identity != 0 || len(graph.owners) != 1 || graph.nextIdentity != 2 || next.resourceRefs != 1 {
		t.Fatal("node exhaustion consumed the concrete guard", err)
	}
	receipt, err := graph.acquirePending(member, true)
	if err != nil || owner.resourceRefs != 2 || graph.live != 1 {
		t.Fatal("retained producer receipt acquisition", err)
	}
	if _, err := graph.acquirePending(member, true); !errors.Is(err, errDependencyCapacity) || owner.resourceRefs != 2 || graph.live != 1 {
		t.Fatal("receipt exhaustion changed the physical guard", err)
	}
	owner.releaseResourceRoot() // exchange the actual bootstrap for the receipt
	if owner.resourceRefs != 1 || owner.resourcesClosed {
		t.Fatal("bootstrap exchange overtook the retained receipt")
	}
	if !receipt.release() || callbacks != 1 || owner.resourceRefs != 0 || !owner.resourcesClosed {
		t.Fatal("exact retained receipt failed physical release")
	}
	if err := graph.unregister(member); err != nil {
		t.Fatal(err)
	}
	// Capacity rejection never consumed this other concrete bootstrap acquisition.
	if err := graph.registerRetained(nextMember); err != nil || nextMember.identity != 2 {
		t.Fatal("capacity retry lost its retained producer identity", err)
	}
	next.releaseResourceRoot()
	if err := graph.unregister(nextMember); err != nil || callbacks != 2 {
		t.Fatal("retry guard did not drain exactly once", err)
	}
}
